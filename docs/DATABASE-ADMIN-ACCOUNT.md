# The panel's own database account

R-057. This document records a decision about ownership, because the code that
follows from it is small and the decision behind it is not.

## The defect this answers

CelikPanel installs MariaDB or PostgreSQL through its own service flow, and
then cannot use the engine it just installed. It keeps a root password for
every database server and never sets one. A freshly packaged MariaDB admits
`root` only through the machine's unix socket; a freshly packaged PostgreSQL
leaves the `postgres` role with no password and accepts local peer
authentication. Neither will accept the empty credential the panel holds.

Until now the panel said so honestly and then asked for something it could not
be given:

> Give this server a root password, then have an administrator register the
> server in CelikPanel again with that password.

There is no register-a-server screen. The list is filled by autodiscovery,
which inserts rows with no credential at all, and deleting a row races
autodiscovery into recreating it. The instruction was reachable only through
the admin API. A panel that tells the operator to do something the panel does
not let them do is worse than one that says nothing.

## The decision

**The panel opens its own administrative account. It never touches the
operator's root.**

The account is called `celikpanel_admin`. The panel generates its password,
seals it, and connects as that account from then on. Whatever the operator's
own way into the engine is — a root password they set, unix socket
authentication, peer authentication — it is exactly as it was before
CelikPanel arrived, and exactly as it will be after CelikPanel is removed.

### Why the panel is allowed to do this

The agent runs as root on that machine. On a freshly packaged MariaDB it can
open a `mysql` session as `root@localhost` over the unix socket with no
password; on a freshly packaged PostgreSQL it can run `psql` as the `postgres`
system user through peer authentication. Those are the doors the machine's
administrator already has, and the panel *is* the machine's administrator —
this is not a credential it obtained by surprise. It is precisely what a human
administrator would type at that shell.

What separates this from what a control panel must not do is the next step.
Having opened that door, the panel creates a **new** identity for itself and
walks through as that. It does not set a root password, does not change one,
and does not record one. The operator's root account is not read, not written,
and not depended on.

### Why not simply set a root password and keep it

Because the operator may already own that account and rely on it. A panel that
sets root's password behind the operator's back breaks their access to their
own machine, silently, and there is no honest place to tell them afterwards.
The failure is invisible until the operator needs root and finds it changed —
which is exactly when they least want a surprise.

The dedicated account gives the panel everything it needs and gives the
operator two things this alternative cannot: their root works, and revoking
the panel's access is one statement they can type themselves.

## What the account is granted

The grant is stated plainly rather than described as least-privilege, because
on one of the two engines it is not.

**PostgreSQL** — `LOGIN CREATEDB CREATEROLE`, and not `SUPERUSER`. This is
genuinely less than the `postgres` role: it cannot read arbitrary files, load
extensions, or bypass row-level security. The panel creates every database it
manages, so it owns them, and ownership carries the rest of what it needs.

**MariaDB** — `ALL PRIVILEGES ON *.* WITH GRANT OPTION`. MariaDB has no
smaller shape that still lets an account create databases, create users, and
grant those users rights, which is the whole of what the panel does. In power
this is root-equivalent, and calling it anything else would be a comfortable
lie. What it still buys, and the reason to do it anyway:

- it is a **separate identity**, so the audit trail on that engine
  distinguishes what the panel did from what a person did;
- it is **revocable in one statement** without the operator changing their own
  password, which is the difference between removing the panel's access and
  locking themselves out;
- the operator's root **keeps working**, which is the whole point.

## Engines the panel did not install

The local privileged path only exists on the machine the agent runs on, and
only while the engine still admits socket or peer authentication. A remote
engine, or one whose local doors have been closed, cannot be given an account
this way — and should not be, because that machine is somebody else's.

For those, an administrator supplies a credential — a username and a password —
on the server itself. This is the other half of the answer, and it is what
turns the old dead-end sentence into a followable one: there is now somewhere
to put the password the message asks for.

## When the account is opened

At the moment the panel first claims the engine, and only then.

The server list is filled by autodiscovery, which inserts a row for every
database engine it finds running on the machine. That row is the panel saying
"I can manage this" — and until now the claim was false, because the row was
born with no credential and a packaged engine refuses the empty one. So the
account is opened there: when the row is created, once.

Once, and not on every listing. An engine whose local doors have been closed
on purpose cannot be given an account, and asking it again every time somebody
opens the databases page would be a panel that nags about a decision the
administrator has already made. An administrator can ask again explicitly, and
the server's page offers it.

A failure does not fail autodiscovery. The engine is installed and the row is
correct either way; a list that will not render because of a credential would
be a worse thing to do than a card that says the account is not there yet.

## What an administrator can see and do

The password is the panel's, but it is not the panel's secret to keep from the
person who owns the machine.

- An administrator can **see** the current password.
- An administrator can **change** it — the panel applies the new password to
  the engine and reseals it, so the stored credential and the engine never
  disagree.
- An administrator can **remove** the account. The panel forgets the
  credential at the same time: keeping a password for an account it has been
  told to give up would be keeping a secret for nothing.

Reading the password is an administrator action and is recorded as one. A
credential that can be read without a trace is a credential nobody can
account for.

## What this does not change

- The operator's root password on any engine. Not set, not changed, not read.
- Databases and users the panel did not create.
- Any engine the panel cannot reach through a local privileged path, unless an
  administrator hands it a credential on purpose.

## What the operator reads when it has not happened

The refusal sentences changed with this. They used to end:

> Give this server a root password, then have an administrator register the
> server in CelikPanel again with that password.

That was honest about the product's position and useless as an instruction,
because there is no register-a-server screen to carry it out on. An instruction
with nowhere to be followed is worse than none. They now name the thing the
administrator can actually do, in CelikPanel, and they still say what the panel
will not touch — because that is the reason there is a separate account at all.
