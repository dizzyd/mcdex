# mcdex - Minecraft Modpack Management

mcdex is a command-line utility that runs on Linux, Windows and OSX 
and makes it easy to manage your modpacks while using the native
Minecraft launcher.

## Downloads

You can find the most recent releases here:

* [Windows](http://files.mcdex.net/releases/win32/mcdex.exe)
* [Linux](http://files.mcdex.net/releases/linux/mcdex)
* [OSX](http://files.mcdex.net/releases/osx/mcdex)

## Getting started

First, make sure you have the most recent database of mods:

```
mcdex db.update
```

## Installing a modpack from Curseforge

Let's install the [Age of Engineering](https://minecraft.curseforge.com/projects/age-of-engineering) modpack.

Find the modpack URL on Curseforge. Then on the console, run:
```
mcdex pack.install aoe1.0.3 https://minecraft.curseforge.com/projects/age-of-engineering/files/2446286
```

Note that we provide the name "aoe1.0.3"; mcdex uses this to install the modpack into your Minecraft home directory
under ```<minecraft>/mcdex/pack/aoe1.0.3```. Alternatively, you can control what directory the modpack is installed in by passing
an absolute path (e.g. `c:\aoe1.0.3` or `/Users/dizzyd/aoe1.0.3`) as the name and mcdex will use that instead.

Once the install is done, you can fire up the Minecraft launcher and you should have a new profile for the aoe1.0.3 pack!

## Creating a new modpack

We can start a new modpack by using the ```pack.create``` command:

```
mcdex pack.create mypack 1.11.2
```

Note that the recommended version of Forge is installed automatically. If you want to force a specific Forge to be used,
you can do
```
mcdex pack.create mypack 1.11.2 13.20.1.2386
```

As before, since we passed a non-absolute filename - 'mypack' - the pack will be created under the Minecraft home directory. 
If we wanted to create the modpack in our home directory (on OSX), we would do:

```
mcdex pack.create /Users/dizzyd/mypack 1.11.2
```

```pack.create``` will create the directory, make sure the appropriate version of Forge is installed and start a manifest.json. 
In addition, it will create an entry in the Minecraft launcher so you can launch the pack.

## Installing individual mods

Once you have a modpack, either installed from CurseForge or one you created locally, you can add individual mods to it. Let's
add [Immersive Engineering](https://minecraft.curseforge.com/projects/immersive-engineering) to our new pack:

```
mcdex mod.select mypack 'Immersive Engineering'
```

This will search the database of mods for one named 'Immersive Engineering' and find the most recent stable version and
add it to the pack's manifest.jason. To actually install the mod, you need to install the pack:

```
mcdex pack.install mypack
```

## Listing available mods

If you want to find all the mods with 'Map' in the name, you can do:

```
mcdex mod.list Map
```

Alternatively, if you want to only look for mods with 'Map' that work on 1.10.2, you can do:

```
mcdex mod.list Map 1.10.2
```
