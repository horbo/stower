#!/usr/bin/env bash
set -euo pipefail

root=${1:-/tmp/stower-demo}
case "$root" in
  *stower-demo*) ;;
  *) echo "refusing to use $root: the path must contain 'stower-demo'" >&2; exit 1 ;;
esac

rm -rf "$root"
mkdir -p "$root"
root=$(cd "$root" && pwd -P)

home="$root/home"
repo="$home/dotfiles"

mkdir -p "$home/.config/nvim" "$home/.config/ghostty" "$home/.local/bin" "$home/Documents" "$home/Projects"

printf 'export EDITOR=nvim\nalias gs="git status"\n' > "$home/.zshrc"
printf '[user]\n\tname = Ada Lovelace\n' > "$home/.gitconfig"
printf 'set -g mouse on\n' > "$home/.tmux.conf"
printf 'set number\n' > "$home/.vimrc"
printf 'vim.opt.number = true\n' > "$home/.config/nvim/init.lua"
printf 'format = "$directory$git_branch"\n' > "$home/.config/starship.toml"
printf 'theme = dark\n' > "$home/.config/ghostty/config"

mkdir -p "$repo/zsh" "$repo/git" "$repo/nvim/dot-config/nvim" "$repo/tmux"
mv "$home/.zshrc" "$repo/zsh/dot-zshrc"
mv "$home/.gitconfig" "$repo/git/dot-gitconfig"
mv "$home/.config/nvim/init.lua" "$repo/nvim/dot-config/nvim/init.lua"
rmdir "$home/.config/nvim"
mv "$home/.tmux.conf" "$repo/tmux/dot-tmux.conf"

stow -d "$repo" -t "$home" --dotfiles zsh git nvim

rm "$home/.gitconfig"
printf '[user]\n\tname = Ada Lovelace\n\temail = ada@example.com\n' > "$home/.gitconfig"

printf 'export PATH="$HOME/.local/bin:$PATH"\n' > "$repo/zsh/.zprofile"

git -C "$repo" init -q -b main
git -C "$repo" add -A
git -C "$repo" -c user.name=demo -c user.email=demo@example.com commit -qm "zsh, git, nvim, tmux"
printf 'alias ll="ls -la"\n' >> "$repo/zsh/dot-zshrc"

echo "$root"
