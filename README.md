# mcdex - Minecraft Modpack Management

mcdex is a command-line utility that runs on Linux, Windows and OSX 
and makes it easy to manage your modpacks while using the native
Minecraft launcher.

## Installing a modpack from Curseforge

Let's install the [Age of Engineering](https://minecraft.curseforge.com/projects/age-of-engineering) modpack.

First, find the modpack file on Curseforge. In this case, we'll use: 
https://minecraft.curseforge.com/projects/age-of-engineering/files/2446286

Now, on the command prompt/console run: 
```
mcdex installPack aoe1.0.3 https://minecraft.curseforge.com/projects/age-of-engineering/files/2446286
```
Note that we provide the name "aoe1.0.3"; mcdex uses this to install the modpack into your Minecraft home directory
under <minecraft>/mcdex/pack/aoe1.0.3. If you want to control what directory the modpack is installed in, you
can pass an absolute path (e.g. c:\aoe1.0.3 or /Users/username/aoe1.0.3) as the name and mcdex will use that instead.

Once the install is done, you can fire up the Minecraft launcher and you should have a new profile for the aoe1.0.3 pack!

## Downloads

You can find the most recent releases here:

* [Windows](http://files.mcdex.net/releases/win32/mcdex.exe)
* [Linux](http://files.mcdex.net/releases/linux/mcdex)
* [OSX](http://files.mcdex.net/releases/osx/mcdex)