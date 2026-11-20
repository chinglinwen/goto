#!/usr/bin/env bash
# Link this repo's agent skill into the local skills directory.
# Idempotent: existing symlinks are replaced, real directories are backed up.
set -euo pipefail

skill_name="goto-remote-command"
repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
src="$repo_root/docs/skills/$skill_name"

if [ ! -f "$src/SKILL.md" ]; then
	echo "missing $src/SKILL.md" >&2
	exit 1
fi

install_into() {
	local dir="$1"
	local target="$dir/$skill_name"
	mkdir -p "$dir"
	if [ -L "$target" ]; then
		rm -f "$target"
	elif [ -e "$target" ]; then
		local backup="$target.bak.$(date +%Y%m%d%H%M%S)"
		mv "$target" "$backup"
		echo "backed up existing $target -> $backup"
	fi
	ln -s "$src" "$target"
	echo "linked $target -> $src"
}

# ~/.claude/skills is the shared location. ~/.agents/skills is often a symlink
# to it, so resolve first and install once per real directory.
dirs="$HOME/.claude/skills"
if [ -d "$HOME/.agents" ]; then
	dirs="$dirs $HOME/.agents/skills"
fi
if [ -n "${SKILL_DIRS:-}" ]; then
	dirs="$SKILL_DIRS"
fi

seen=""
for dir in $dirs; do
	real="$(readlink -f "$dir" 2>/dev/null || echo "$dir")"
	case " $seen " in
	*" $real "*) continue ;;
	esac
	seen="$seen $real"
	install_into "$real"
done