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

Pre-requirements:
```
1. PostgreSQL server
2. Go 1.26.2 or higher
```

1. Create the .env file
2. Put your PostgreSQL credentials
3. Start the server with `go run .`
4. Enjoy!

# II. How to create an admin account

By default, there is no admin account. You will have to go manually create an invite code in the PostgreSQL database:
```sql
INSERT INTO admin_invitations (token, used) VALUES ('YOUR_INVITE_CODE', false);
```

Then you can access through: http://YOUR_SERVER_IP:3333/admin/register and create an account :)

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

The files are stored unecrypted in the folder `./files/`.

User data, file data... etc are stored in a PostgreSQL database.

Here is the structure of the database:
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

