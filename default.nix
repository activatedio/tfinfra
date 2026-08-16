with import <nixpkgs> {};

stdenv.mkDerivation {
  name = "tfinfra";
  buildInputs = with pkgs; [
    go
    gnumake
    buf
    protoc-gen-go
  ];
  shellHook = ''
    export GOPATH=$HOME/go
    export PATH=$PATH:$GOPATH/bin
  '';
  hardeningDisable = [ "fortify" ];
}
