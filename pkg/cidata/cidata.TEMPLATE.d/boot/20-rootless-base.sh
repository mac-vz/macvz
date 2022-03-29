##!/bin/sh
#set -eux
#
## This script does not work unless systemd is available
#command -v systemctl >/dev/null 2>&1 || exit 0
#
## Set up env
#for f in .profile .bashrc; do
#	if ! grep -q "# Macvz BEGIN" "/home/${MACVZ_CIDATA_USER}.linux/$f"; then
#		cat >>"/home/${MACVZ_CIDATA_USER}.linux/$f" <<EOF
## Macvz BEGIN
## Make sure iptables and mount.fuse3 are available
#PATH="\$PATH:/usr/sbin:/sbin"
#export PATH
#EOF
#		cat >>"/home/${MACVZ_CIDATA_USER}.linux/$f" <<EOF
## Macvz END
#EOF
#		chown "${MACVZ_CIDATA_USER}" "/home/${MACVZ_CIDATA_USER}.linux/$f"
#	fi
#done
## Enable cgroup delegation (only meaningful on cgroup v2)
#if [ ! -e "/etc/systemd/system/user@.service.d/macvz.conf" ]; then
#	mkdir -p "/etc/systemd/system/user@.service.d"
#	cat >"/etc/systemd/system/user@.service.d/macvz.conf" <<EOF
#[Service]
#Delegate=yes
#EOF
#fi
#systemctl daemon-reload
#
## Set up sysctl
#sysctl_conf="/etc/sysctl.d/99-macvz.conf"
#if [ ! -e "${sysctl_conf}" ]; then
#	if [ -e "/proc/sys/kernel/unprivileged_userns_clone" ]; then
#		echo "kernel.unprivileged_userns_clone=1" >>"${sysctl_conf}"
#	fi
#	echo "net.ipv4.ping_group_range = 0 2147483647" >>"${sysctl_conf}"
#	echo "net.ipv4.ip_unprivileged_port_start=0" >>"${sysctl_conf}"
#	sysctl --system
#fi
#
## Set up subuid
#for f in /etc/subuid /etc/subgid; do
#	grep -qw "${MACVZ_CIDATA_USER}" $f || echo "${MACVZ_CIDATA_USER}:100000:65536" >>$f
#done
#
## Start systemd session
#systemctl start systemd-logind.service
#loginctl enable-linger "${MACVZ_CIDATA_USER}"
