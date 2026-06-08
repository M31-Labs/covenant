# Covenant

**The safe cryptocurrency contract language. Unrug-pullable by default.**

Minting is first-class — but *mint-and-dump is impossible by default.* Every power that could rug a counterparty (mint, drain, freeze, fee, upgrade) is **off unless you loudly declare it with hard bounds**, and a quiet rug is a **compile error**. The compiler emits a **rug-surface report** — a machine-checked *trust badge* a project shows its holders.

> Mission: legitimize crypto contracts and minting by making safety **provable and disclosed by construction** — so the default flips from *"assume scam until audited"* to *"show me the proof."*

Pure Go. No CGo. Built on the [gotreesitter](https://github.com/odvcencio/gotreesitter) `grammargen` substrate. Cross-compiles anywhere Go does, including `wasip1/wasm`.

---

## The trust badge

A community token where minting is capped and quorum-gated:

```
contract CommunityToken {
    ledger balances: TOKEN
    supply total:    TOKEN
    invariant conserves(balances, total)

    mint issue cap 1_000_000 TOKEN by approval 2 of { founder, treasurer, community } {
        credit recipient : amount
    }
    transition transfer(to: Account, amount: TOKEN) by (caller owns amount) {
        move balances: caller -> to : amount
    }
    burn retire by (caller owns amount) { debit caller : amount }
}
```

```console
$ covenant rugsurface examples/community_token.cov

🛡  CommunityToken — rug-surface report

   mint "issue":  capped 1,000,000 TOKEN  ·  requires 2-of-3 { founder, treasurer, community }

   ✓ no hidden mint        ✓ no discretionary payout
   ✓ no freeze             ✓ no fee
   ✓ supply hard-capped

   → a holder can verify: nobody mints past the cap, nobody mints without quorum, no backdoor.
   proof sha256:2f48ec08f368be41
```

The same report has a canonical machine-readable form for wallets, explorers,
token pages, and CI:

```console
$ covenant rugsurface examples/community_token.cov --json
{
  "schema": "m31labs.covenant.rug_surface.v1",
  "contract": "CommunityToken",
  "mints": [
    {
      "name": "issue",
      "cap": 1000000,
      "unit": "TOKEN",
      "quorum": 2,
      "signers": ["founder", "treasurer", "community"]
    }
  ],
  "no_hidden_mint": true,
  "no_discretionary_payout": true,
  "no_freeze": true,
  "no_fee": true,
  "supply_hard_capped": true,
  "safe": true,
  "empty_rug_surface": false
}
```

The guarantees are enforced, not advisory:

```console
$ covenant run … issue  amount=500000  --approvals=founder,treasurer   # within cap, 2-of-3
  ✓ OK   Δ alice +500,000   supply +500,000   receipt f2258e88…

$ covenant run … issue  amount=2000000 --approvals=founder,treasurer   # over the 1M cap
  ✗ rejected: MINT_CAP_EXCEEDED                                        # …state untouched

$ covenant run … issue  amount=500000  --approvals=founder            # only one approver
  ✗ rejected: MINT_QUORUM_UNMET
```

A contract that tries to mint *quietly* (a `transition` that creates supply) doesn't compile:

```

Nor can a transition hide a drain behind net-zero supply math. In v1,
capability lanes are strict: `transition` bodies move value from `caller`,
`mint` bodies only `credit`, and `burn` bodies only `debit caller`. A
`debit victim; credit caller` transition is still a compile error even though
total supply nets to zero.

A mint that omits its hard cap also parses into the checker and fails with a
teaching diagnostic (`MINT_UNCAPPED`) instead of degrading into a generic syntax
error.
❌ this transition creates or destroys value  (line 8, col 9)
   why:  a transition that changes total supply is a hidden mint/burn — the most common way tokens get rugged
   fix:  use `move` to transfer existing value, or put supply changes in a declared `mint`/`burn` capability
```

---

## How it works

A contract is an explicit, governed, audited **state machine over money-typed state** with **total value conservation** and **atomic settlement**. The pipeline:

```
.cov ─grammar→ CST ─lower→ IR ─check→ {diagnostics, rug-surface report}
                              └─interp→ atomic execution → deterministic receipt
```

- **Conservation is proven by capability deltas, not author-written holder loops.** A keyed `ledger` exposes only structured ops (`move`/`credit`/`debit`); the compiler proves each action stays in its declared lane, so value can't be created, destroyed, or reassigned through hidden `credit`/`debit` tricks except through a declared, **capped + quorum-gated** capability. The v1 interpreter also does a defensive in-memory invariant recheck before commit.
- **Atomic & deterministic runtime.** Every transition runs on a state snapshot: authority → quorum → cap → apply → re-verify conservation → commit-or-revert. No wall-clock, no I/O; the same inputs always yield the same byte-identical receipt.
- **The badge never lies.** An unsafe contract renders `⚠ UNSAFE`, not a clean check.

## Quickstart

```sh
go build ./cmd/covenant
covenant check       examples/community_token.cov
covenant rugsurface  examples/community_token.cov
covenant run         examples/community_token.cov issue recipient=alice amount=500000 \
                       --caller=founder --now=1 --approvals=founder,treasurer
```

Run the tests (the thesis is a regression gate in `acceptance_test.go`):

```sh
go test ./...
```

> **Build note:** v1 builds against a local checkout of `gotreesitter` via a `go.mod` `replace`. Pinning a published version for clean external builds is a tracked follow-up.

## Status — v1 (tracer bullet)

A safe mintable community token end-to-end: grammar → IR → conservation + mint-safety checks + rug-surface report → atomic interpreter + receipts → CLI. Pure-Go, cross-compiles to `wasip1/wasm` and `linux/arm64`.

**Scope:** single-instance, trusted-operator tokens; the guarantee is a **language-level** one (you cannot *author* a quiet rug). It is **not** trustless yet, and not DeFi.

## Roadmap

The guarantee strengthens up an enforcement ladder:

1. **Language (here, v1)** — you cannot *author* a quiet rug; clean contracts are provably power-free.
2. **+ Anchoring** — operator-signed, hash-chained receipts anchored to an external ledger → rugs become *detectable and attributable*.
3. **+ Verifiable execution** — a chain / ZK-proven VM / replicated runtime makes the guarantee *enforced at runtime*, even against a dishonest operator. This is where "legitimize all crypto" lands.

Also queued: a bytecode VM (with the current interpreter as the differential-proof oracle); the agreement/state-machine shape (escrow, vesting, auctions) with governed gates and migration; exact-decimal money types; and a published verifier for the canonical rug-surface JSON proof.
