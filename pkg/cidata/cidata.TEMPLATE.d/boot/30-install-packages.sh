#!/bin/sh
set -eux

INSTALL_IPTABLES=0
if [ "${MACVZ_CIDATA_UDP_DNS_LOCAL_PORT}" -ne 0 ] || [ "${MACVZ_CIDATA_TCP_DNS_LOCAL_PORT}" -ne 0 ]; then
	INSTALL_IPTABLES=1
fi

# Install minimum dependencies
if command -v apt-get >/dev/null 2>&1; then
	pkgs=""
	if [ "${INSTALL_IPTABLES}" = 1 ] && [ ! -e /usr/sbin/iptables ]; then
		pkgs="${pkgs} iptables"
	fi
	if [ -n "${pkgs}" ]; then
		DEBIAN_FRONTEND=noninteractive
		export DEBIAN_FRONTEND
		apt-get update
		# shellcheck disable=SC2086
		apt-get install -y --no-upgrade --no-install-recommends -q ${pkgs}
	fi
elif command -v dnf >/dev/null 2>&1; then
	pkgs=""
	if ! command -v tar >/dev/null 2>&1; then
		pkgs="${pkgs} tar"
	fi
	if [ "${INSTALL_IPTABLES}" = 1 ] && [ ! -e /usr/sbin/iptables ]; then
		pkgs="${pkgs} iptables"
	fi
	if [ -n "${pkgs}" ]; then
		dnf_install_flags="-y --setopt=install_weak_deps=False"
		if grep -q "Oracle Linux Server release 8" /etc/system-release; then
			# repo flag instead of enable repo to reduce metadata syncing on slow Oracle repos
			dnf_install_flags="${dnf_install_flags} --repo ol8_baseos_latest --repo ol8_codeready_builder"
		elif grep -q "release 8" /etc/system-release; then
			dnf_install_flags="${dnf_install_flags} --enablerepo powertools"
		fi
		# shellcheck disable=SC2086
		dnf install ${dnf_install_flags} ${pkgs}
	fi
elif command -v apk >/dev/null 2>&1; then
	pkgs=""
	if [ "${INSTALL_IPTABLES}" = 1 ] && ! command -v iptables >/dev/null 2>&1; then
		pkgs="${pkgs} iptables"
	fi
	if [ -n "${pkgs}" ]; then
		apk update
		# shellcheck disable=SC2086
		apk add ${pkgs}
	fi
fi

SETUP_DNS=0
if [ -n "${MACVZ_CIDATA_UDP_DNS_LOCAL_PORT}" ] && [ "${MACVZ_CIDATA_UDP_DNS_LOCAL_PORT}" -ne 0 ]; then
	SETUP_DNS=1
fi
if [ -n "${MACVZ_CIDATA_TCP_DNS_LOCAL_PORT}" ] && [ "${MACVZ_CIDATA_TCP_DNS_LOCAL_PORT}" -ne 0 ]; then
	SETUP_DNS=1
fi
if [ "${SETUP_DNS}" = 1 ]; then
	# Try to setup iptables rule again, in case we just installed iptables
	"${MACVZ_CIDATA_MNT}/boot/09-host-dns-setup.sh"
fi