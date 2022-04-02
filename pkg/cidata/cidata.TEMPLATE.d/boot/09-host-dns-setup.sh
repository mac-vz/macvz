#!/bin/sh
set -eux

# Wait until iptables has been installed; 30-install-packages.sh will call this script again
if command -v iptables >/dev/null 2>&1; then
	if [ -n "${MACVZ_CIDATA_UDP_DNS_LOCAL_PORT}" ] && [ "${MACVZ_CIDATA_UDP_DNS_LOCAL_PORT}" -ne 0 ]; then
		# Only add the rule once
		if ! iptables-save | grep "udp.*${MACVZ_CIDATA_SLIRP_GATEWAY}:${MACVZ_CIDATA_UDP_DNS_LOCAL_PORT}"; then
			iptables -t nat -i enp0s1 -A PREROUTING -p udp --dport 53 -j DNAT \
				--to-destination "${MACVZ_CIDATA_SLIRP_GATEWAY}:${MACVZ_CIDATA_UDP_DNS_LOCAL_PORT}"
			iptables -t nat -A OUTPUT -p udp --dport 53 -j DNAT \
				--to-destination "${MACVZ_CIDATA_SLIRP_GATEWAY}:${MACVZ_CIDATA_UDP_DNS_LOCAL_PORT}"
		fi
	fi
	if [ -n "${MACVZ_CIDATA_TCP_DNS_LOCAL_PORT}" ] && [ "${MACVZ_CIDATA_TCP_DNS_LOCAL_PORT}" -ne 0 ]; then
		# Only add the rule once
		if ! iptables-save | grep "tcp.*${MACVZ_CIDATA_SLIRP_GATEWAY}:${MACVZ_CIDATA_TCP_DNS_LOCAL_PORT}"; then
			iptables -t nat -i enp0s1 -A PREROUTING -p tcp --dport 53 -j DNAT \
				--to-destination "${MACVZ_CIDATA_SLIRP_GATEWAY}:${MACVZ_CIDATA_TCP_DNS_LOCAL_PORT}"
			iptables -t nat -A OUTPUT -p tcp --dport 53 -j DNAT \
				--to-destination "${MACVZ_CIDATA_SLIRP_GATEWAY}:${MACVZ_CIDATA_TCP_DNS_LOCAL_PORT}"
		fi
	fi
fi
