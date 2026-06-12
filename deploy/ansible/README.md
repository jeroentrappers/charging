# Ansible deployment

Provisions and deploys the charging stack (Go API + ingest + PWA + PostGIS +
OSRM) behind nginx/TLS. Built to move the workload from **appmire4-web** (arm64,
shared box) to **appmire-hetz1** (amd64, stronger box) without data loss, and to
be reused for migrating the other appmire4-web workloads later.

## Layout

```
ansible.cfg            inventory + vault password file
inventory.ini          [charging] target, [source] = appmire4-web (dump source)
group_vars/all.yml     non-secret vars (domain, repo, images, ports, OSRM)
group_vars/charging/vault.yml(.example)   secrets (.env + Mobilithek mTLS certs)
site.yml               deploy: bootstrap → charging → nginx
migrate-data.yml       one-time DB backup/restore (appmire4-web → target)
roles/bootstrap        docker engine, firewall, base packages
roles/charging         code (rsync/git), secrets, OSRM data, compose up
roles/nginx            reverse proxy + certbot TLS (coexists with other vhosts)
```

## One-time prerequisites

1. **SSH access** — the playbook connects as an unprivileged `ansible` user
   (passwordless sudo for system tasks; added to the `docker` group by the
   bootstrap role). Ensure that user exists and authorize your key
   (`~/.ssh/id_ed25519`):
   ```
   # if the ansible user already exists with your key — done.
   # otherwise, as root on the box:
   #   adduser --disabled-password --gecos '' ansible
   #   usermod -aG sudo ansible && echo 'ansible ALL=(ALL) NOPASSWD:ALL' >/etc/sudoers.d/ansible
   #   install -d -m700 -o ansible -g ansible /home/ansible/.ssh
   #   cp ~/.ssh/authorized_keys /home/ansible/.ssh/ && chown ansible:ansible /home/ansible/.ssh/authorized_keys
   ssh-copy-id -i ~/.ssh/id_ed25519.pub ansible@136.243.103.58   # if password auth is on
   ```
2. **Collections + secrets**:
   ```
   cd deploy/ansible
   ansible-galaxy collection install -r requirements.yml
   cp group_vars/charging/vault.yml.example group_vars/charging/vault.yml
   # fill from appmire4-web:/opt/charging/.env + secrets/, then:
   echo 'YOUR_VAULT_PASSWORD' > .vault-pass && chmod 600 .vault-pass
   ansible-vault encrypt group_vars/charging/vault.yml
   ```
   (`vault.yml`, `.vault-pass`, and `files/` are gitignored — secrets never land
   in the repo. Set `secrets_mode: copy` in `all.yml` to upload plain files from
   `files/{env,mob-cert.pem,mob-key.pem}` instead of vaulting.)

## Deploy

```
cd deploy/ansible
ansible-playbook site.yml                 # full provision + deploy
ansible-playbook site.yml --tags app      # ship a code update only
ansible-playbook site.yml --tags nginx    # vhost / TLS only
```

`--tags app` rsyncs the working tree, re-renders `.env`, rebuilds, and `up -d`s
— so day-to-day updates are one command. OSRM data is prepared only when the
processed graph is missing.

## Data migration (no data loss)

The DB holds the irreplaceable data — `charger_report` (user feedback),
`tariff_version` (SCD2 price history), `charger_status`. Everything else is
re-ingestible. `migrate-data.yml` does a consistent `pg_dump -Fc` on
appmire4-web and `pg_restore`s it onto the target (stopping the target's api +
ingest during restore), then prints row counts.

```
ansible-playbook site.yml            # 1. stand the stack up on the target
ansible-playbook migrate-data.yml    # 2. copy the live data over
# ... verify the site on the (temp) domain ...
ansible-playbook migrate-data.yml    # 3. final sync during cutover window
# ... move charging.appmire.be DNS to the new box, re-run site.yml --tags nginx
#     so certbot issues for the real hostname ...
```

`pg_dump -Fc` is a single consistent snapshot, so the source keeps serving
during the dump; run the final sync right before the DNS flip to minimise the
gap. Re-running is safe — the restore is `--clean --if-exists`.

## Cutover (domain)

Deploy first under a throwaway host (set `charging_domain:
charging-hetz1.appmire.be` in `all.yml`) to validate end-to-end, then point the
real DNS records at the new box, set `charging_domain` back to
`charging.appmire.be`, and run `site.yml --tags nginx` to issue the production
cert. Decommission appmire4-web's charging stack once traffic has moved.
