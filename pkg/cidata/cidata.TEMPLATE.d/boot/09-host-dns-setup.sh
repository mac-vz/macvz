#!/bin/sh
set -eux

export CURRENT_IPADDR=$(hostname -I | awk '{print $1}')
export GATEWAY_IPADDR=$(ip route | grep default | awk '{print $3}')

# Wait until iptables has been installed; 30-install-packages.sh will call this script again
if command -v iptables >/dev/null 2>&1; then
	if [ -n "${MACVZ_CIDATA_UDP_DNS_LOCAL_PORT}" ] && [ "${MACVZ_CIDATA_UDP_DNS_LOCAL_PORT}" -ne 0 ]; then
		# Only add the rule once
		if ! iptables-save | grep "udp.*${CURRENT_IPADDR}:${MACVZ_CIDATA_UDP_DNS_LOCAL_PORT}"; then
			iptables -t nat -A PREROUTING -d ${GATEWAY_IPADDR}/32 -p udp --dport 53 -j DNAT \
				--to-destination "${CURRENT_IPADDR}:${MACVZ_CIDATA_UDP_DNS_LOCAL_PORT}"
			iptables -t nat -A OUTPUT -d ${GATEWAY_IPADDR}/32 -p udp --dport 53 -j DNAT \
				--to-destination "${CURRENT_IPADDR}:${MACVZ_CIDATA_UDP_DNS_LOCAL_PORT}"
		fi
	fi
	if [ -n "${MACVZ_CIDATA_TCP_DNS_LOCAL_PORT}" ] && [ "${MACVZ_CIDATA_TCP_DNS_LOCAL_PORT}" -ne 0 ]; then
		# Only add the rule once
		if ! iptables-save | grep "tcp.*${CURRENT_IPADDR}:${MACVZ_CIDATA_TCP_DNS_LOCAL_PORT}"; then
			iptables -t nat -A PREROUTING -d ${GATEWAY_IPADDR}/32 -p tcp --dport 53 -j DNAT \
				--to-destination "${CURRENT_IPADDR}:${MACVZ_CIDATA_TCP_DNS_LOCAL_PORT}"
			iptables -t nat -A OUTPUT -d ${GATEWAY_IPADDR}/32 -p tcp --dport 53 -j DNAT \
				--to-destination "${CURRENT_IPADDR}:${MACVZ_CIDATA_TCP_DNS_LOCAL_PORT}"
		fi
	fi
fi
