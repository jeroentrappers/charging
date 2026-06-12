#!/bin/bash
# Refresh ufw SSH allow rules from a dynamic DNS name. Ported from the box's
# original nftables ssh_dyndns updater. Rules are tagged with a ufw comment so
# only dyndns-managed rules are pruned — static allow rules are never touched.
# Safe-by-design: if DNS resolution fails, existing rules are left untouched, so
# a DNS outage can never lock admins out.
set -euo pipefail

HOST="${1:-hq.appmire.be}"
PORT="${2:-22}"
TAG="ssh-dyndns"

mapfile -t IPS < <(getent ahostsv4 "$HOST" | awk '{print $1}' | sort -u)
if [ "${#IPS[@]}" -eq 0 ]; then
	echo "no DNS answer for $HOST; leaving ufw rules untouched"
	exit 0
fi

# Ensure a tagged allow rule exists for each current IP (ufw add is idempotent).
for ip in "${IPS[@]}"; do
	ufw allow from "$ip" to any port "$PORT" proto tcp comment "$TAG" >/dev/null
done

# Prune tagged rules whose IP is no longer in the DNS answer. Collect first,
# then delete by number high→low (rule numbers shift as rules are removed).
declare -A keep
for ip in "${IPS[@]}"; do keep["$ip"]=1; done

mapfile -t tagged < <(ufw status numbered | grep -F "# $TAG")
to_delete=()
for line in "${tagged[@]}"; do
	num=$(printf '%s' "$line" | sed -n 's/^\[ *\([0-9]\+\)\].*/\1/p')
	ip=$(printf '%s' "$line" | grep -oE '[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+' | head -1)
	[ -n "$num" ] && [ -n "$ip" ] || continue
	[ -n "${keep[$ip]:-}" ] || to_delete+=("$num")
done
for num in $(printf '%s\n' "${to_delete[@]}" | sort -rn); do
	yes | ufw delete "$num" >/dev/null || true
done

echo "ufw ssh-dyndns ($HOST) -> ${IPS[*]}"
