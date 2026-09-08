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

### Behind a reverse proxy, or with your own PostgreSQL

Copy `docker-compose.override.yml.example` to `docker-compose.override.yml`; Compose
merges it automatically. It covers two cases:

- **reverse proxy** (Nginx Proxy Manager, Traefik, Caddy): stop publishing the port
  and join your proxy network instead;
- **existing PostgreSQL**: set `POSTGRESQL_HOST` in your `.env` and skip the bundled
  database. The bundled service is named `db`, so a container of yours named
  `postgres` is never shadowed.

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

To invite another administrator later, add a new code in the database:

```sql
INSERT INTO admin_invitations (token, used) VALUES ('YOUR_INVITE_CODE', false);
```

```bash
docker compose exec db psql -U filetransfer -d filetransfer
```

# II bis. The admin dashboard

Once logged in, everything happens at `/admin/dashboard`:

- **Overview** — file count, disk usage, share of password-protected files, active
  sessions, registered passkeys;
- **Public files** — list, rename, change ID, delete, and download. Downloading a
  password-protected file from here does **not** ask for its password: an admin
  reads the files off the disk anyway, so this is an assumed privilege. Every such
  download is written to the server log with the admin's username;
- **My cloud** — a private file space per administrator, with folders. Files here
  are stored under `FILES_PATH/personal/u<id>/` and are never served by any public
  route. Ownership is enforced in SQL, so one admin cannot reach another's files;
- **Security** — passkeys, password, login method, and active sessions with remote
  sign-out;
- **Administrators** — list accounts, generate invitation codes from the interface
  instead of by hand in SQL, revoke an account.

### Passkeys

Each administrator picks their own login method under **Security → Method**:

| Setting | Behaviour |
|---|---|
| Password only | the default |
| Passkey or password | sign in with the passkey, password stays as a fallback |
| Password then passkey | both are required (two factors) |

You cannot select a passkey option before registering at least one passkey, and
removing your last passkey drops the account back to password-only — neither of
those can lock you out.

> [!IMPORTANT]
> **Passkeys require HTTPS**, and `PUBLIC_URL` must match the address people
> actually visit (e.g. `PUBLIC_URL=https://files.example.com`). WebAuthn ties every
> credential to that exact hostname; if it disagrees with the browser's address bar,
> the browser refuses the ceremony. Everything else in the dashboard works over
> plain HTTP.

> [!NOTE]
> Sessions are now server-side, carried by an `HttpOnly` cookie. Earlier versions
> kept the admin username and password in ordinary cookies readable by any script
> on the page. Those cookies are cleared on sight — after upgrading, every
> administrator signs in once more.

# III. How to use it

> [!WARNING]
> To tell you how it really works. It just waits for the stream to end ;-; (yeah I do trust people)
> So be careful on who you share it with.

1. Go on http://YOUR_SERVER_IP:3333/
2. Select your file
3. Click on the upload button
4. Wait
5. Share the link to the person you want to share it with
6. Damn it's just a file transfer you know how it works right?
7. Then the person you shared it with has to open the link
8. After he opened the link he has to click on the download button
9. Bro, it's not that hard I swear
10. Do whatever you want with the file then
11. Oh and you can put a password to the file but I guess you already saw that... I hope.

## IV. Stored data

The files are stored unecrypted in the folder `./uploads/` (or in `FILES_PATH` if you run the server without Docker).

User data, file data... etc are stored in a PostgreSQL database.

Administration data lives in four more tables, created automatically on start:
`admin_sessions` (server-side sessions), `admin_credentials` (passkeys),
`personal_files` (each admin's private cloud), and the extra bookkeeping columns on
`admin_invitations`.

Here is the structure of the original tables:
<table>
    <thead>
        <tr>
            <th>file_transfer</th>
            <th>users</th>
            <th>admin_invitations</th>
        </tr>
    </thead>
    <tbody>
        <tr>
            <td>id <strong>— VARCHAR(6) PRIMARY KEY</strong></td>
            <td>id <strong>— SERIAL PRIMARY KEY</strong></td>
            <td>token <strong>— VARCHAR(255) NOT NULL</strong></td>
        </tr>
        <tr>
            <td>file_name <strong>— VARCHAR(255) NOT NULL</strong></td>
            <td>email_address <strong>— VARCHAR(255) NOT NULL</strong></td>
            <td>token <strong>— VARCHAR(255) NOT NULL</strong></td>
        </tr>
        <tr>
            <td>file_size <strong>— BIGINT NOT NULL</strong></td>
            <td>last_name <strong>— VARCHAR(255) NOT NULL</strong></td>
            <td></td>
        </tr>
        <tr>
            <td>ip_addr <strong>— VARCHAR(45) NOT NULL</strong></td>
            <td>first_name <strong>— VARCHAR(255) NOT NULL</strong></td>
            <td></td>
        </tr>
        <tr>
            <td>date <strong>— TIMESTAMP DEFAULT CURRENT_TIMESTAMP</strong></td>
            <td>username <strong>— VARCHAR(255) NOT NULL</strong></td>
            <td></td>
        </tr>
        <tr>
            <td>has_passwd <strong>— BOOLEAN DEFAULT</strong></td>
            <td>password <strong>— VARCHAR(255) NOT NULL</strong></td>
            <td></td>
        </tr>
        <tr>
            <td>xpasswd <strong>— CHAR(64)</strong></td>
            <td>token <strong>— VARCHAR(255) NOT NULL</strong></td>
            <td></td>
        </tr>
        <tr>
            <td>salt_passwd <strong>— BYTEA</strong></td>
            <td>confirmed <strong>— BOOLEAN DEFAULT FALSE</strong></td>
            <td></td>
        </tr>
        <tr>
            <td>id <strong>— VARCHAR(6) PRIMARY KEY</strong></td>
            <td>invitation_used <strong>— VARCHAR(255) NOT NULL</strong></td>
            <td></td>
        </tr>
        <tr>
            <td></td>
            <td>confirmation_code <strong>— VARCHAR(255) NOT NULL</strong></td>
            <td></td>
        </tr>
    </tbody>
</table>

