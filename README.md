# mcdex - Minecraft Modpack Management

mcdex is a command-line utility that runs on Linux, Windows and OSX 
and makes it easy to manage your modpacks while using the native
Minecraft launcher.

## Downloads

You can find the most recent releases here:

* [Windows](http://files.mcdex.net/releases/win32/mcdex.exe)
* [Linux](http://files.mcdex.net/releases/linux/mcdex)
* [OSX](http://files.mcdex.net/releases/osx/mcdex)

## Installing a modpack from Curseforge

Let's install the [Age of Engineering](https://minecraft.curseforge.com/projects/age-of-engineering) modpack.

First, find the modpack URL on Curseforge. Then on the console, run:
```
mcdex installPack aoe1.0.3 https://minecraft.curseforge.com/projects/age-of-engineering/files/2446286
```

Note that we provide the name "aoe1.0.3"; mcdex uses this to install the modpack into your Minecraft home directory
under ```<minecraft>/mcdex/pack/aoe1.0.3```. Alternatively, you can control what directory the modpack is installed in by passing
an absolute path (e.g. `c:\aoe1.0.3` or `/Users/dizzyd/aoe1.0.3`) as the name and mcdex will use that instead.

Once the install is done, you can fire up the Minecraft launcher and you should have a new profile for the aoe1.0.3 pack!

## Creating a new modpack

We can start a new modpack by using the ```createPack``` command:

```
mcdex createPack mypack 1.11.2 13.20.1.2386
```

Again, since we passed a non-absolute filename, the pack will be created under the Minecraft home directory. If we wanted to create
the modpack in our home directory (on OSX), we would do:

```
mcdex createPack /Users/dizzyd/mypack 1.11.2 13.20.1.2386
```

```createPack``` will create the directory, make sure the appropriate version of Forge is installed and start a manifest.json. 
In addition, it will create an entry in the Minecraft launcher so you can launch the pack.

## Installing individual mods

Once you have a modpack, either installed from CurseForge or one you created locally, you can add individual mods to it. Let's
add [Immersive Engineering](https://minecraft.curseforge.com/projects/immersive-engineering) to our new pack:

```
mcdex registerMod mypack https://minecraft.curseforge.com/projects/immersive-engineering/files/2443553
```

This will add the entry for the file to your manifest.json, but not immediately download anything.. To actually download the mod, 
you need to install the pack:

```
mcdex installPack mypack
```


