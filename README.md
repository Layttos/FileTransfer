# FileTransfer

> [!IMPORTANT]
> This project is under the license **CC BY-NC-SA 4.0**.<br>
> [![CC BY-NC-SA 4.0](https://mirrors.creativecommons.org/presskit/buttons/88x31/svg/by-nc-sa.svg)](http://creativecommons.org/licenses/by-nc-sa/4.0/)

> [!CAUTION]
>This project was strictly created by me and for my personal use with friends, to keep control of my own data.
> This project has also seen the day because I wanted to send data with an unlimited size. Be strictly careful to who you share this to.
> The files stored through this have no expiration date and are stored unencrypted on the disk.

> [!NOTE]
> If you wish to check some other projects I have done, please check my Github page.
> Check this [Apple Music Rich Presence made for macOS users](https://github.com/Layttos/AppleMusic-RichPresence) or this [Discord bot](https://github.com/Layttos/CiaoKombucha)

Self-hosted file transfer with no size limit, plus an administration panel:

- **Transfers** streamed straight to disk, resumable, optionally password-protected
- **One-command deployment**, PostgreSQL included, no configuration file needed
- **Admin dashboard** with an audit journal, storage usage, and file search
- **Personal cloud** per administrator, with a file explorer, media preview and a
  code editor
- **Passkeys** (WebAuthn) as an alternative or a second factor
- **Documented HTTP API** at `/api`

# I. How to setup

Only requirement: **Docker** (with the Compose plugin).

```bash
git clone https://github.com/Layttos/FileTransfer.git
cd FileTransfer
docker compose up -d
```

That's it. Compose builds the app, starts a PostgreSQL database next to it, creates
the tables and exposes everything on <http://localhost:3333>.

Nothing to configure: no `.env` is required. If you want to change the port, the
database credentials or the timezone, copy `.env.example` to `.env` and edit it
**before** the first `docker compose up`.

Useful commands:

```bash
docker compose logs -f filetransfer   # follow the logs
docker compose down                   # stop (keeps files and database)
docker compose down -v                # stop and ERASE the database
```

Uploaded files land in `./uploads/` on the host, the database lives in the
`db-data` Docker volume. Both survive `docker compose down` and rebuilds.

### Configuration

Everything is optional. Copy `.env.example` to `.env` to change any of it, before
the first `docker compose up`.

| Variable | Default | Role |
|---|---|---|
| `PORT` | `3333` | Port published on the host |
| `POSTGRESQL_DATABASE` / `_USER` / `_PASSWORD` | `filetransfer` | Bundled database credentials. Only read when the volume is first created |
| `POSTGRESQL_HOST` | `db` | Point it elsewhere to use your own PostgreSQL |
| `PUBLIC_URL` | `http://localhost:3333` | Public address. Anchors passkeys, so it must match what visitors type |
| `STORAGE_KEY` | generated on first start | Storage encryption key, 64 hex characters. Left empty, a key is written to `FILES_PATH/.storage-key` |
| `STORAGE_MAX` | disk capacity | Ceiling shown by the storage bar (`500G`, `2T`, …) |
| `AUDIT_RETENTION_DAYS` | `90` | Journal retention. `0` never purges |
| `TZ` | `Europe/Paris` | Timezone |

### Behind a reverse proxy, or with your own PostgreSQL

Create a `docker-compose.override.yml` next to `docker-compose.yml`; Compose merges
it automatically and the file is gitignored, so it stays yours.

- **reverse proxy** (Nginx Proxy Manager, Traefik, Caddy): stop publishing the port
  and join your proxy network instead;
- **existing PostgreSQL**: set `POSTGRESQL_HOST` in your `.env` and skip the bundled
  database. The bundled service is named `db`, so a container of yours named
  `postgres` is never shadowed.

Both at once:

```yaml
services:
  filetransfer:
    ports: !override []
    depends_on: !override []
    networks:
      - default
      - proxy

  # Do not start the bundled database.
  db:
    scale: 0

networks:
  proxy:
    external: true
    name: your-proxy-network
```

> [!IMPORTANT]
> **A reverse proxy will cap your uploads long before this app does.** The server
> itself streams the request straight to disk and has no size limit — a 4 GiB upload
> was verified end to end. But nginx defaults to a small `client_max_body_size`
> (Nginx Proxy Manager ships `2000m`), and anything bigger is rejected with a `413`
> that never reaches the app.

For nginx or Nginx Proxy Manager, add this to the host configuration (in NPM: your
proxy host → **Advanced** tab):

```nginx
# No size limit on uploads
client_max_body_size 0;

# Stream straight to the app instead of spooling the whole file
# to the proxy's own disk before forwarding a single byte
proxy_request_buffering off;
proxy_buffering off;

# A multi-gigabyte transfer runs far past the 90s defaults
client_body_timeout 3600s;
send_timeout 3600s;
proxy_connect_timeout 60s;
proxy_send_timeout 3600s;
proxy_read_timeout 3600s;
```

Other proxies: Traefik has no body limit by default, but raise its
`respondingTimeouts`. Caddy has none either. Cloudflare's proxy caps request bodies
(100 MB on free plans) and cannot be raised — put large-file uploads on a
DNS-only record.

### Without Docker

Pre-requirements: a PostgreSQL server and Go 1.26 or higher.

1. Copy `.env.example` to `.env` and fill in your PostgreSQL credentials
2. Uncomment `POSTGRESQL_HOST`, `POSTGRESQL_PORT` and `FILES_PATH` at the bottom of the file
3. Start the server with `go run .`

# II. How to create an admin account

Nothing manual to do — **on the very first start, the server prints an invitation
code in its own logs**:

```bash
docker compose logs filetransfer
```

```
================================================================
| PREMIER DEMARRAGE - aucun compte administrateur              |
|                                                              |
|   Code d'invitation :  KP57S-NPXTE-WMQZX-AXK7B               |
|                                                              |
|   Inscription :  http://localhost:3333/admin/register        |
|                                                              |
| Ce code est a usage unique. Il reste affiche a chaque        |
| demarrage tant qu'aucun compte n'a ete cree.                 |
================================================================
```

Go to <http://localhost:3333/admin/register>, paste the code, and your admin
account is created.

The code is single-use. It stays visible on every restart until an account is
actually created, so losing the first log output is not a problem. Once an admin
exists, the banner never shows up again.

To invite another administrator later, generate a code from the dashboard under
**Administrators → Invitation codes**. No SQL needed any more; the hand-written
`INSERT` earlier versions required still works if you prefer it.

# II bis. The admin dashboard

Once logged in, everything happens at `/admin/dashboard`. It works on a phone as
well as on a desktop: bottom navigation within thumb's reach, cards instead of
tables, and touch targets sized accordingly.

### Overview

File count, storage used against the cap, share of password-protected files,
active sessions, registered passkeys.

The storage bar reads its ceiling from `STORAGE_MAX` (`500G`, `2T`, …) when set,
and otherwise from the real capacity of the filesystem holding `FILES_PATH`. It
breaks the total down between public transfers and personal clouds.

### Public files

List, rename, change ID, delete, download.

Search accepts a plain substring, matched against the file name, the short ID and
the uploader's IP — and also shell-style patterns: `*.zip` finds every archive,
`photo?.jpg` matches a single character. A file whose name literally contains
`*.zip` is found by that same search.

> [!NOTE]
> Downloading a password-protected file from the dashboard does **not** ask for
> its password. An administrator reads the files off the disk anyway, so this is
> an assumed privilege — and every such download is recorded in the journal with
> the account name.

### My cloud

A private file space per administrator, behaving like a real file explorer.

**Right-click** (or **long-press** on a phone) opens a context menu:

| Where | Actions |
|---|---|
| Empty space | New folder, new file, upload, download the current folder as `.zip`, go up, refresh |
| A folder | Open, download as `.zip`, rename, upload into it, delete with its contents |
| A file | Preview or edit, download, open in the browser, share publicly, share behind a password, rename, move, copy the private link, delete |

Double-clicking opens a file or enters a folder.

**Images, videos, audio and PDFs** open in a full-screen viewer. **Text and source
files** open in an editor with syntax highlighting for around 150 languages
detected from the file name, line numbers, search, code folding and bracket
matching. `Ctrl+S` saves, up to 2 MiB per file. The editor is loaded from a CDN
like Tailwind; if that fails, it falls back to a plain text area so editing still
works.

**Sharing** publishes a personal file into the public transfer space, with an
optional password. Where the filesystem allows it the file is shared by hard link
rather than copied, so it costs no extra disk space and deleting one side leaves
the other intact.

Personal files live under `FILES_PATH/personal/u<id>/` and are never served by any
public route. Ownership is enforced in SQL, so one admin cannot reach another's
files.

### Journal

Every action on the site is recorded: uploads, downloads (public and
administrative), refused file passwords, sign-ins and failures, passkeys,
sessions, password and login-method changes, file and folder operations, sharing,
accounts, invitations, server start.

Filter by category and level, or search across the actor, the target, the detail
and the IP. Counters cover the last 24 hours. Writing is asynchronous, so the
journal can never slow down or fail a transfer. Entries are kept 90 days by
default, configurable with `AUDIT_RETENTION_DAYS`.

### Security

Passkeys, password, login method, and active sessions with remote sign-out.

### Administrators

List accounts, generate invitation codes from the interface instead of by hand in
SQL, revoke an account.

### Passkeys

Each administrator picks their own login method under **Security → Method**:

| Setting | Behaviour |
|---|---|
| Password only | the default |
| Passkey or password | sign in with the passkey, password stays as a fallback |
| Password then passkey | both are required (two factors) |

Registering your first passkey moves the account to *passkey or password* on its
own, so a freshly registered key works straight away. You cannot select a passkey
option without one registered, and removing your last passkey drops the account
back to password-only — none of these paths can lock you out.

Credentials are requested as resident keys, which is what lets password managers
such as Bitwarden store them as real passkeys. The sign-in page also arms
conditional mediation, so your passkey is offered directly in the username field
without clicking anything.

> [!IMPORTANT]
> **Passkeys require HTTPS**, and `PUBLIC_URL` must match the address people
> actually visit (e.g. `PUBLIC_URL=https://files.example.com`). WebAuthn ties every
> credential to that exact hostname; if it disagrees with the browser's address bar,
> the browser refuses the ceremony. Everything else in the dashboard works over
> plain HTTP.

> [!NOTE]
> Sessions are server-side, carried by an `HttpOnly` cookie whose hash alone is
> stored. Versions before 2.0 kept the admin username and password in ordinary
> cookies readable by any script on the page; those are cleared on sight, so every
> administrator signs in once more after upgrading.

# II ter. HTTP API

Every endpoint is documented at **`/api`**, reachable without signing in and
linked from the site footer: the public upload and download routes, the single
`/admin/api` entry point with its actions, file transfers, the WebAuthn
ceremonies, status codes and limits.

# III. How to use it

> [!WARNING]
> To tell you how it really works. It just waits for the stream to end ;-; (yeah I do trust people)
> So be careful on who you share it with.

1. Go on `https://your-server/`
2. Pick your files — several at once works
3. Optionally set a password on them
4. Hit upload and wait
5. Share the link you get back
6. Damn it's just a file transfer you know how it works right?
7. The person opens the link and clicks download
8. Bro, it's not that hard I swear

Uploads and downloads are streamed, so size is bounded only by your disk and by
your reverse proxy. Downloads support range requests, which means a large
transfer can be resumed and a video can be seeked.

## IV. Stored data

Uploaded files are stored **encrypted** in `./uploads/` (or in `FILES_PATH`
without Docker). Each transfer gets its own directory named after its short ID.
Personal clouds live beside them, under `personal/u<id>/`.

Everything else lives in PostgreSQL. All tables are created and migrated
automatically on start, so upgrading needs no manual SQL.

| Table | Holds |
|---|---|
| `file_transfer` | Public transfers: short ID, name, size, uploader IP, date, password hash and salt |
| `users` | Administrator accounts, bcrypt password, and the chosen login method (`auth_policy`) |
| `admin_invitations` | Invitation codes, whether they were used, by whom and when |
| `admin_sessions` | Server-side sessions. Only the SHA-256 of the token is stored, so a database leak cannot be replayed as a session |
| `admin_credentials` | Passkeys, kept as the full WebAuthn credential in JSON so a library upgrade cannot break the mapping |
| `personal_files` | Files in each administrator's private cloud |
| `personal_folders` | Folders of those clouds, including empty ones |
| `audit_log` | The journal: level, category, action, actor, IP, target, detail, user agent |

### Encryption at rest

Every stored file is encrypted with **AES-256-GCM**, in 1 MiB frames, each frame
sealed under a nonce derived from its index — so frames cannot be reordered, and
the last one is marked, so the file cannot be silently truncated. Each file gets
its own key derived from the master key and a per-file salt.

Files that compress are also compressed with **zstd**, decided by probing the
first frame. Already-compressed content — video, archives, photos — is stored raw,
because compressing it gains nothing and would cost throughput.

Uncompressed frames all have the same length, so a byte offset maps to a frame
arithmetically: **range requests keep working**, and so do resumable downloads and
seeking inside a video.

Measured on a Haswell core with AES-NI: AES-256-GCM runs at 2.6 GB/s, well above
what disk and network deliver. Serving a 1.5 GiB file went from ~2400 MB/s to
~605 MB/s on loopback with a warm cache — the untouched path uses `sendfile()`,
which encryption necessarily gives up — but still comfortably above a real network
path. Uploads are within a few percent of the plaintext figure.

> [!IMPORTANT]
> **What this protects against.** A stolen disk, a leaked backup, a misplaced copy
> of the storage directory. It does **not** protect against someone who controls
> the running server: the key is there, by design, because previews, the editor and
> administrative downloads all need to read the files.
>
> If `STORAGE_KEY` is not set, the key is generated next to the data it protects,
> which means a backup of the folder carries both. Put it in the environment and
> delete the file for the protection to be real.

> [!NOTE]
> A file's password protects its share link, not its content against whoever runs
> the machine. Passwords are hashed with a per-file salt.

Files uploaded before encryption was enabled are converted in the background on
start, and stay readable throughout: the reader accepts both formats.
