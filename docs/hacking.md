# hacking on tangled

We highly recommend [installing
nix](https://nixos.org/download/) (the package manager)
before working on the codebase. The nix flake provides a lot
of helpers to get started and most importantly, builds and
dev shells are entirely deterministic.

To set up your dev environment:

```bash
nix develop
```

Non-nix users can look at the `devShell` attribute in the
`flake.nix` file to determine necessary dependencies.

## running the appview

The nix flake also exposes a few `app` attributes (run `nix
flake show` to see a full list of what the flake provides),
one of the apps runs the appview with the `air`
live-reloader:

```bash
TANGLED_DEV=true nix run .#watch-appview

# TANGLED_DB_PATH might be of interest to point to
# different sqlite DBs

# in a separate shell, you can live-reload tailwind
nix run .#watch-tailwind
```

To authenticate with the appview, you will need redis and
OAUTH JWKs to be setup:

```
# oauth jwks should already be setup by the nix devshell:
echo $TANGLED_OAUTH_JWKS
{"crv":"P-256","d":"tELKHYH-Dko6qo4ozYcVPE1ah6LvXHFV2wpcWpi8ab4","kid":"1753352226","kty":"EC","x":"mRzYpLzAGq74kJez9UbgGfV040DxgsXpMbaVsdy8RZs","y":"azqqXzUYywMlLb2Uc5AVG18nuLXyPnXr4kI4T39eeIc"}

# if not, you can set it up yourself:
go build -o genjwks.out ./cmd/genjwks
export TANGLED_OAUTH_JWKS="$(./genjwks.out)"

# run redis in at a new shell to store oauth sessions
redis-server
```

## running a knot

An end-to-end knot setup requires setting up a machine with
`sshd`, `AuthorizedKeysCommand`, and git user, which is
quite cumbersome. So the nix flake provides a
`nixosConfiguration` to do so.

To begin, head to `http://localhost:3000/knots` in the browser
and create a knot with hostname `localhost:6000`. This will
generate a knot secret. Set `$TANGLED_VM_KNOT_SECRET` to it,
ideally in a `.envrc` with [direnv](https://direnv.net) so you
don't lose it.

You will also need to set the `$TANGLED_VM_SPINDLE_OWNER`
variable to some value. If you don't want to [set up a
spindle](#running-a-spindle), you can use any placeholder
value. 

You can now start a lightweight NixOS VM using
`nixos-shell` like so:

```bash
nix run .#vm
# or nixos-shell --flake .#vm

# hit Ctrl-a + c + q to exit the VM
```

This starts a knot on port 6000, a spindle on port 6555
with `ssh` exposed on port 2222. You can push repositories
to this VM with this ssh config block on your main machine:

```bash
Host nixos-shell
    Hostname localhost
    Port 2222
    User git
    IdentityFile ~/.ssh/my_tangled_key
```

Set up a remote called `local-dev` on a git repo:

```bash
git remote add local-dev git@nixos-shell:user/repo
git push local-dev main
```

## running a spindle

You will need to find out your DID by entering your login handle into
<https://pdsls.dev/>. Set `$TANGLED_VM_SPINDLE_OWNER` to your DID.

The above VM should already be running a spindle on `localhost:6555`.
You can head to the spindle dashboard on `http://localhost:3000/spindles`,
and register a spindle with hostname `localhost:6555`. It should instantly
be verified. You can then configure each repository to use this spindle
and run CI jobs.

Of interest when debugging spindles:

```
# service logs from journald:
journalctl -xeu spindle

# CI job logs from disk:
ls /var/log/spindle

# debugging spindle db:
sqlite3 /var/lib/spindle/spindle.db

# litecli has a nicer REPL interface:
litecli /var/lib/spindle/spindle.db
```
