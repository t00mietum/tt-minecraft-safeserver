<!-- markdownlint-disable MD007 -- Unordered list indentation -->
<!-- markdownlint-disable MD010 -- No hard tabs -->
<!-- markdownlint-disable MD055 -- Table pipe style [Expected: leading_and_trailing; Actual: leading_only; Missing trailing pipe] -->
<!-- markdownlint-disable MD041 -- First line in a file should be a top-level heading -->

<!-- TOC ignore:true -->
# How to connect

How to join a server that runs on tt-minecraft-safeserver, from home or from anywhere else.

From outside the home, players connect with an internet domain name that the person who runs the server gives them. The examples use `mc.example.com` for that name, and `192.168.1.50` for the server's address at home.

<!-- TOC ignore:true -->
## Table of contents

<!-- TOC -->

- [Before you start](#before-you-start)
- [Which edition do you have](#which-edition-do-you-have)
- [Java Edition on a PC](#java-edition-on-a-pc)
	- [Java from anywhere](#java-from-anywhere)
	- [Java at home](#java-at-home)
- [Bedrock on a phone, tablet, or PC](#bedrock-on-a-phone-tablet-or-pc)
	- [Bedrock from anywhere](#bedrock-from-anywhere)
	- [Bedrock at home](#bedrock-at-home)
- [Bedrock on a console](#bedrock-on-a-console)
- [When it doesn't work](#when-it-doesnt-work)
- [For the person running the server](#for-the-person-running-the-server)

<!-- /TOC -->

## Before you start

- The server only lets in players who are on its whitelist, so send the person who runs it your name first.
	- Java Edition players send their Minecraft username.
	- Bedrock players send their Xbox gamertag.

- You don't need a VPN or any other app. A VPN on your phone or PC can get in the way at home, so turn it off if you can't connect.

## Which edition do you have

| Device                                                              | Edition
| :---                                                                | :---
| Windows, Mac, or Linux, from the Minecraft Launcher, "Java Edition" | Java
| iPhone, iPad, or Android                                            | Bedrock
| Windows, from the Microsoft Store, "Minecraft for Windows"          | Bedrock
| Xbox, PlayStation, or Switch                                        | Bedrock

Both editions play on the same server, in the same world.

## Java Edition on a PC

### Java from anywhere

1. Start Minecraft, and pick Multiplayer.

2. Pick Add Server.

3. Type a name you'll recognize for Server Name, and `mc.example.com` for Server Address.

4. Pick Done, then pick the server in the list and pick Join Server.

- When the server uses a port other than 25565, add it to the address with a colon, such as `mc.example.com:25570`.

- Your game version should match the server's. When it doesn't match, the server list shows the version it wants, in red.

### Java at home

- Do the same as above, with the home address such as `192.168.1.50` in place of `mc.example.com`.

- The internet address may also work at home. That depends on the home router, so use the home address if it doesn't.

- Java Edition doesn't list servers on the local network by itself. That list is only for single player worlds opened to LAN.

## Bedrock on a phone, tablet, or PC

### Bedrock from anywhere

1. Start Minecraft, and pick Play.

2. Open the Servers tab, scroll to the bottom, and pick Add Server.

3. Fill in the form.
	- Type a name you'll recognize for Server Name.
	- Type `mc.example.com` for Server Address.
	- Leave Port at `19132`, unless you were given another one.

4. Pick Save, then pick the server to join.

- The first time, you may be asked to sign in to your Microsoft account. The server checks that sign-in, so a guest account won't work.

- The first time you join, the game may ask whether to trust the server. Pick yes when the address is the one you were given.

### Bedrock at home

- Add the server on the Servers tab as above, with the home address such as `192.168.1.50` and port `19132`.

- The server doesn't show up under LAN Games on the Friends tab yet, so don't wait for it there.

## Bedrock on a console

- Xbox, PlayStation, and Switch don't let you add a server by its address, and the server doesn't show up under LAN Games yet. So for now, consoles need BedrockConnect, at home or away.

- A console that uses BedrockConnect can join from its server list. Type the server's domain name and port `19132` there.

- A later version of tt-minecraft-safeserver may let consoles join through the Friends tab instead.

## When it doesn't work

| What you see                                              | What to try
| :---                                                      | :---
| "You are not white-listed on this server"                 | Ask to be added. Bedrock players need their gamertag added, not a Java username
| "Outdated client" or "Outdated server"                    | Update Minecraft, or ask which version the server runs
| "Can't connect to server" or "Unable to connect to world" | Check the address and port. Turn off any VPN. Try again in a few minutes, in case the server is restarting
| The server doesn't show under LAN Games                   | That's expected for now. Add it by its home address on the Servers tab
| "Failed to verify username" on Java                       | Restart the Minecraft Launcher and sign in again
| You were kicked with "Server closed"                      | The server is restarting. Wait a minute and join again

## For the person running the server

- Add a Java player:

~~~bash
tt-minecraftd cmd whitelist add Steve
~~~

- Add a Bedrock player, by gamertag:

~~~bash
tt-minecraftd cmd fwhitelist add "Some Gamertag"
~~~

- For players from outside the home, the server needs `allowFrom` set to `any`, and either a router port forward or a tunnel. Forward TCP 25565 for Java, and both TCP and UDP 19132 for Bedrock. [design.md](project/design.md#exposure) has the details.
