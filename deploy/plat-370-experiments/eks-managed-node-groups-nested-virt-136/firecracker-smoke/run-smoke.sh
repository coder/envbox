#!/bin/sh
set -eu

api_socket=/tmp/firecracker.socket
log_file=/tmp/firecracker.log
rootfs_path=/var/lib/firecracker/rootfs.ext4
rm -f "$api_socket" "$log_file"

if [ "${FIRECRACKER_COPY_ROOTFS:-false}" = "true" ]; then
  rootfs_path=/tmp/rootfs.ext4
  cp /var/lib/firecracker/rootfs.ext4 "$rootfs_path"
fi

network_enabled=${FIRECRACKER_NETWORK:-false}
if [ "$network_enabled" = "true" ]; then
  ip tuntap add dev tap0 mode tap
  ip addr add 172.16.0.1/24 dev tap0
  ip link set tap0 up
fi

/usr/local/bin/firecracker --api-sock "$api_socket" >"$log_file" 2>&1 &
firecracker_pid=$!
if [ "$network_enabled" = "true" ]; then
  trap 'ip link del tap0 2>/dev/null || true; kill "$firecracker_pid" 2>/dev/null || true' EXIT
else
  trap 'kill "$firecracker_pid" 2>/dev/null || true' EXIT
fi

for _ in $(seq 1 50); do
  if [ -S "$api_socket" ]; then
    break
  fi
  sleep 0.1
done

test -S "$api_socket"

put() {
  path="$1"
  data="$2"
  curl --fail-with-body --silent --show-error \
    --unix-socket "$api_socket" \
    -X PUT \
    -H 'Content-Type: application/json' \
    --data "$data" \
    "http://localhost${path}"
}

put /machine-config '{"vcpu_count":1,"mem_size_mib":128,"smt":false}'
boot_args='console=ttyS0 reboot=k panic=1 pci=off'
if [ "$network_enabled" = "true" ]; then
  boot_args="$boot_args ip=172.16.0.2::172.16.0.1:255.255.255.0::eth0:off"
fi
put /boot-source "{\"kernel_image_path\":\"/var/lib/firecracker/vmlinux\",\"boot_args\":\"$boot_args\"}"
put /drives/rootfs "{\"drive_id\":\"rootfs\",\"path_on_host\":\"${rootfs_path}\",\"is_root_device\":true,\"is_read_only\":false}"
if [ "$network_enabled" = "true" ]; then
  put /network-interfaces/eth0 '{"iface_id":"eth0","host_dev_name":"tap0"}'
fi
put /actions '{"action_type":"InstanceStart"}'

echo 'FIRECRACKER_INSTANCE_START_SUCCEEDED'
sleep 3

echo '--- Firecracker log ---'
cat "$log_file"
