# FileTransfer

> [!CAUTION]
>This project was strictly created by me and for my personal use with friends, to keep control of my own data.
> This project has also seen the day because I wanted to send data with an unlimited size. Be strictly careful to who you share this to.
> The files stored through this have no expiration date and are stored unencrypted on the disk.

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


