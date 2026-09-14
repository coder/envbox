# shellcheck shell=sh
# shellcheck disable=SC2154 # envbox_max_attempts is prepended by the Go caller

# cgroup v2: enable nesting. Mirrors moby's hack/dind L61-79
# (https://github.com/moby/moby/blob/8d9e3502aba39127e4d12196dae16d306f76993d/hack/dind#L61-L79),
# bounded by envbox_max_attempts.
if [ -f /sys/fs/cgroup/cgroup.controllers ]; then
	# Some runtimes already root /sys/fs/cgroup at "/" after unsharing the
	# cgroup namespace; remounting it again fails with EBUSY. Only remount
	# when it's still rooted at a nested host path.
	# When mounts are stacked at /sys/fs/cgroup, the visible mount is the
	# one whose ID is not another same-location mount's parent (see
	# proc_pid_mountinfo(5)).
	get_cgroup_mount_root() {
		awk '
			$5 == "/sys/fs/cgroup" { root[$1] = $4; isparent[$2] = 1 }
			END { for (id in root) if (!(id in isparent)) print root[id] }
		' /proc/self/mountinfo
	}
	configure_cgroup_nesting=true
	if [ "$(get_cgroup_mount_root)" != "/" ]; then
		# Remount /sys/fs/cgroup so the new cgroup namespace's view becomes the
		# fs root; inner container cgroups end up under the envbox container's
		# cgroup on the host. Starting dockerd with potentially incorrect cgroup
		# attribution is preferable to failing the workspace if neither unmount
		# method can detach the inherited mount.
		# A regular unmount can fail with EBUSY on runtimes that retain references
		# to the inherited mount. A lazy detach keeps retained references valid
		# while freeing the mount point for a correctly rooted replacement.
		if ! umount /sys/fs/cgroup; then
			echo "envbox: normal umount of /sys/fs/cgroup failed; trying lazy detach" >&2
			if ! umount -l /sys/fs/cgroup; then
				configure_cgroup_nesting=false
				echo "envbox: failed to detach /sys/fs/cgroup; skipping cgroup nesting setup (inner container cgroup attribution may be incorrect)" >&2
			fi
		fi
		if [ "$configure_cgroup_nesting" = true ]; then
			mount -t cgroup2 cgroup /sys/fs/cgroup || { echo "envbox: failed to mount cgroup2 on /sys/fs/cgroup" >&2; exit 1; }
			cgroup_mount_root=$(get_cgroup_mount_root)
			if [ "$cgroup_mount_root" != "/" ]; then
				configure_cgroup_nesting=false
				echo "envbox: cgroup2 mount root is '$cgroup_mount_root' after remount; skipping cgroup nesting setup (inner container cgroup attribution may be incorrect)" >&2
			fi
		fi
	fi

	if [ "$configure_cgroup_nesting" = false ]; then
		# exec replaces this shell, so the nesting setup below is not run.
		exec "$0" "$@"
	fi

	# move the processes from the root group to the /init group,
	# otherwise writing subtree_control fails with EBUSY.
	# An error during moving non-existent process (i.e., "cat") is ignored.
	mkdir -p /sys/fs/cgroup/init || { echo "envbox: failed to mkdir /sys/fs/cgroup/init" >&2; exit 1; }
	# this happens in a loop because things like "docker exec" on our dind
	# container will create new processes, which creates a race between our
	# moving everything to "init" and enabling subtree_control
	envbox_attempts=0
	while ! {
		# move the processes from the root group to the /init group,
		# otherwise writing subtree_control fails with EBUSY.
		# An error during moving non-existent process (i.e., "cat") is ignored.
		xargs -rn1 < /sys/fs/cgroup/cgroup.procs > /sys/fs/cgroup/init/cgroup.procs || :
		# enable controllers
		sed -e 's/ / +/g' -e 's/^/+/' < /sys/fs/cgroup/cgroup.controllers \
			> /sys/fs/cgroup/cgroup.subtree_control
	}; do
		envbox_attempts=$((envbox_attempts + 1))
		if [ "$envbox_attempts" -ge "$envbox_max_attempts" ]; then
			echo "envbox: failed to enable cgroup.subtree_control after $envbox_attempts attempts" >&2
			exit 1
		fi
	done
fi
exec "$0" "$@"
