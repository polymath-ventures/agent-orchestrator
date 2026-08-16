# Worktree recipe

Rule 2 states the invariant: every change you make — issue-tracked or ad-hoc,
code or docs or config — happens in a worktree YOU created under the repo-local
agent worktree directory. This is how you create one:

**This worktree is disposable.** When its PR merges, cleanup deletes the whole
directory — tracked, untracked, and ignored alike, without asking. Anything that
must survive has to be committed and pushed before then; nothing left here is
recoverable.

```bash
create_owned_worktree() {
  main_repo_root="$1"
  target_worktree="$2"
  branch_arg="$3" # new branch name, an existing branch, or --detach
  start_ref="$4" # ref for new/detached checkouts; empty means check out branch_arg
  work_item_key="$5"
  canon_worktree=
  test -n "$work_item_key" || {
    echo "Cannot create an owned worktree without a work item key" >&2
    return 1
  }
  if [ "$branch_arg" = "--detach" ]; then
    git -C "$main_repo_root" worktree add --detach "$target_worktree" "$start_ref" >&2 || {
      echo "Task worktree creation failed; the session anchor remains untouched" >&2
      return 1
    }
  elif [ -z "$start_ref" ]; then
    git -C "$main_repo_root" worktree add "$target_worktree" "$branch_arg" >&2 || {
      echo "Task worktree creation failed; the session anchor remains untouched" >&2
      return 1
    }
  else
    git -C "$main_repo_root" worktree add "$target_worktree" -b "$branch_arg" "$start_ref" >&2 || {
      echo "Task worktree creation failed; the session anchor remains untouched" >&2
      return 1
    }
  fi
  canon_worktree="$(cd "$target_worktree" && pwd -P)" || {
    git -C "$main_repo_root" worktree remove "$target_worktree" >&2 || true
    echo "Cannot canonicalize the created task worktree" >&2
    return 1
  }
  target_worktree="$canon_worktree"
  target_git_dir="$(git -C "$target_worktree" rev-parse --absolute-git-dir)" || {
    git -C "$main_repo_root" worktree remove "$target_worktree" >&2 || true
    echo "Cannot resolve the created worktree git dir" >&2
    return 1
  }
  printf 'format=polypowers-worktree-owner-v1\ntask=%s\npath=%s\n# ephemeral: this entire directory is deleted when task=%s'"'"'s PR merges\n' \
    "$work_item_key" "$target_worktree" "$work_item_key" \
    >"$target_git_dir/polypowers-worktree-owner" || {
    git -C "$main_repo_root" worktree remove "$target_worktree" >&2 || true
    echo "Cannot record worktree ownership marker" >&2
    return 1
  }
  printf '%s\n' "$target_worktree"
}

MAIN_REPO_ROOT="$(
  git worktree list --porcelain |
    awk '$1 == "worktree" { print substr($0, 10); exit }'
)"
MAIN_REPO_ROOT="$(cd "$MAIN_REPO_ROOT" && pwd -P)"
test "$(git -C "$MAIN_REPO_ROOT" rev-parse --is-inside-work-tree)" = true || {
  echo "Cannot resolve the registered main checkout: $MAIN_REPO_ROOT" >&2
  exit 1
}
DEFAULT_BRANCH_REF="$(git symbolic-ref --quiet refs/remotes/origin/HEAD 2>/dev/null || echo refs/remotes/origin/main)"
DEFAULT_BRANCH="${DEFAULT_BRANCH_REF#refs/remotes/origin/}"
TASK_WORKTREE="$MAIN_REPO_ROOT/.claude/worktrees/<slug>"
WORK_ITEM_KEY=gh:#N
git -C "$MAIN_REPO_ROOT" fetch origin "$DEFAULT_BRANCH"
TASK_WORKTREE="$(create_owned_worktree \
  "$MAIN_REPO_ROOT" "$TASK_WORKTREE" <branch> "origin/$DEFAULT_BRANCH" "$WORK_ITEM_KEY")" || exit 1
test -n "$TASK_WORKTREE" || {
  echo "Owned worktree helper returned an empty path" >&2
  exit 1
}
```

Resolve and target the registered main checkout as above even when the
session was launched inside another worktree, then install dependencies in
the new task checkout. Fetch and branch from the remote ref even when the
local default branch appears clean; a clean local branch can still be stale.
`.claude/worktrees/` is the shared convention for Claude, Codex,
Gemini, and other agents; the `.claude` path name is historical, not a
Claude-only boundary. Do not place working copies under `.git/worktrees/` —
that is Git's private metadata directory for linked worktrees. Derive the
default branch — don't assume `main`. **The shared main checkout root is
read-only ground truth**:
never commit or switch branches there, and treat its files as read-only
during ordinary task work — other agents (and the user) rely on its state.
The `cleanup-merge` lifecycle is the one narrow exception: it may
fast-forward the worktree that already owns the default branch only after
confirming that checkout is clean, and it must never switch that checkout's
branch. Fetch-only sync of refs is always fine. A launcher- or
harness-supplied worktree is the resumable session anchor, regardless of
client or whether it is detached. It may have been created from stale local
state before session-start logic ran. Never remove the session anchor, or
reset, move, or adopt that supplied worktree as the disposable task
worktree; use it only as launch context and create the required task worktree
from the freshly fetched remote ref as above.

The helper call is not optional. It records lifecycle ownership in the new
worktree's git directory as `polypowers-worktree-owner`, which is how other
skills know which work item a worktree belongs to and whether it is safe to
reuse. It is provenance only, and it is not what authorizes deletion: cleanup
removes a merged task worktree whether or not it is marked, and retains an
unmerged one whether or not it is marked. Cleanup will never backfill a missing
marker, because provenance has to be recorded at creation to mean anything. Set
`WORK_ITEM_KEY` to the canonical work-item key `gh:#N`. A plausible path,
branch name, current checkout, or files already written there are never
ownership proof.
