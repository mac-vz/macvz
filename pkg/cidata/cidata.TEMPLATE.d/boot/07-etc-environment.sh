#!/bin/sh
set -eux

# /etc/environment must be written after 05-persistent-data-volume.sh has run to
# make sure the changes on a restart are applied to the persisted version.

if [ -e /etc/environment ]; then
	sed -i '/#MACVZ-START/,/#MACVZ-END/d' /etc/environment
fi
cat "${MACVZ_CIDATA_MNT}/etc_environment" >>/etc/environment

# It is possible that a requirements script has started an ssh session before
# /etc/environment was updated, so we need to kill it to make sure it will
# restart with the updated environment before "linger" is being enabled.

if command -v loginctl >/dev/null 2>&1; then
	loginctl terminate-user "${MACVZ_CIDATA_USER}" || true
fi

# Make sure the guestagent socket from a previous boot is removed before we open the "macvz-ssh-ready" gate.
rm -f /run/macvz-guest-agent.sock

# Signal that provisioning is done. The instance-id in the meta-data file changes on every boot,
# so any copy from a previous boot cycle will have different content.
cp "${MACVZ_CIDATA_MNT}"/meta-data /run/macvz-ssh-ready
