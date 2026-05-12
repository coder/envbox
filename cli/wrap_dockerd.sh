# shellcheck shell=sh
# shellcheck disable=SC2154 # envbox_max_attempts is prepended by the Go caller

# cgroup v2: enable nesting. Mirrors moby's hack/dind L61-79
# (https://github.com/moby/moby/blob/8d9e3502aba39127e4d12196dae16d306f76993d/hack/dind#L61-L79),
# bounded by envbox_max_attempts.
if [ -f /sys/fs/cgroup/cgroup.controllers ]; then
	# Remount /sys/fs/cgroup so the new cgroup namespace's view becomes the
	# fs root; inner container cgroups end up under the envbox container's
	# cgroup on the host.
	umount /sys/fs/cgroup || { echo "envbox: failed to umount /sys/fs/cgroup" >&2; exit 1; }
	mount -t cgroup2 cgroup /sys/fs/cgroup || { echo "envbox: failed to mount cgroup2 on /sys/fs/cgroup" >&2; exit 1; }

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
