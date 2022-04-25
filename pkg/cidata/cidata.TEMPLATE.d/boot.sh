#!/bin/sh
set -eu

INFO() {
	echo "MACVZ| $*"
}

WARNING() {
	echo "MACVZ| WARNING: $*"
}

whoami
INFO "Resizing"
resize2fs /dev/vda

# shellcheck disable=SC2163
while read -r line; do export "$line"; done <"${MACVZ_CIDATA_MNT}"/macvz.env

CODE=0

# Don't make any changes to /etc or /var/lib until boot/05-persistent-data-volume.sh
# has run because it might move the directories to /mnt/data on first boot. In that
# case changes made on restart would be lost.

for f in "${MACVZ_CIDATA_MNT}"/boot/*; do
	INFO "Executing $f"
	if ! "$f"; then
		WARNING "Failed to execute $f"
		CODE=1
	fi
done

if [ -d "${MACVZ_CIDATA_MNT}"/provision.system ]; then
	for f in "${MACVZ_CIDATA_MNT}"/provision.system/*; do
		INFO "Executing $f"
		if ! "$f"; then
			WARNING "Failed to execute $f"
			CODE=1
		fi
	done
fi

USER_SCRIPT="/home/${MACVZ_CIDATA_USER}.linux/.macvz-user-script"
if [ -d "${MACVZ_CIDATA_MNT}"/provision.user ]; then
	if [ ! -f /sbin/openrc-init ]; then
		until [ -e "/run/user/${MACVZ_CIDATA_UID}/systemd/private" ]; do sleep 3; done
	fi
	for f in "${MACVZ_CIDATA_MNT}"/provision.user/*; do
		INFO "Executing $f (as user ${MACVZ_CIDATA_USER})"
		cp "$f" "${USER_SCRIPT}"
		chown "${MACVZ_CIDATA_USER}" "${USER_SCRIPT}"
		chmod 755 "${USER_SCRIPT}"
		if ! sudo -iu "${MACVZ_CIDATA_USER}" "XDG_RUNTIME_DIR=/run/user/${MACVZ_CIDATA_UID}" "${USER_SCRIPT}"; then
			WARNING "Failed to execute $f (as user ${MACVZ_CIDATA_USER})"
			CODE=1
		fi
		rm "${USER_SCRIPT}"
	done
fi

# Signal that provisioning is done. The instance-id in the meta-data file changes on every boot,
# so any copy from a previous boot cycle will have different content.
cp "${MACVZ_CIDATA_MNT}"/meta-data /run/macvz-boot-done

INFO "Exiting with code $CODE"
exit "$CODE"
