{ nixpkgs ? import <nixpkgs> { } }:

let pkgs = [
  nixpkgs.go
  nixpkgs.awscli
];

in nixpkgs.stdenv.mkDerivation {
  name = "env";
  buildInputs = pkgs;
}
