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
  "total_mint_cap": 1000000,
  "recipients_runtime_bound": true,
  "safe": true,
  "empty_rug_surface": false
}
```

`total_mint_cap` is the holder's max-inflation number — the most new supply the
contract can ever create across **every** declared mint (`-1` = unbounded).
`recipients_runtime_bound` discloses that mint destinations are set at call time,
so the badge cannot prove *where* minted tokens go.

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
error. The same lane rejects every *deceptive* mint surface: a single-signer
mint (`MINT_QUORUM_TOO_LOW` — quorum must be ≥ 2 so no one key mints alone), a
quorum larger than the signer set (`MINT_IMPOSSIBLE_QUORUM`), a duplicated signer
that inflates the N-of-M (`MINT_DUPLICATE_SIGNERS`), a zero cap that can never
fire (`MINT_DEAD`), and a mint whose unit doesn't match the supply
(`MINT_UNIT_MISMATCH`).
❌ this transition creates or destroys value  (line 8, col 9)
   why:  a transition that changes total supply is a hidden mint/burn — the most common way tokens get rugged
   fix:  use `move` to transfer existing value, or put supply changes in a declared `mint`/`burn` capability
```

---

## Verify the badge yourself — don't trust, check

A badge only flips the market default if the counterparty can **independently**
confirm it. `covenant verify` recomputes the rug-surface proof from source and
checks it against the issuer's published proof — so a holder, wallet, explorer,
or CI confirms the badge corresponds to the source with **zero trust in the
issuer.**

```console
$ covenant verify CommunityToken.cov --proof badge.json
🔍 CommunityToken — proof verification
   source sha256:         ebec6bf3a92a7f29…
   computed proof sha256: 88b91b763b42a90d…
   claimed proof sha256:  88b91b763b42a90d…
   ✓ VERIFIED — this source produces exactly the claimed rug-surface proof, and it is safe.
```

It catches the obvious fraud — a project that shows a clean badge but ships a
different, draining contract:

```console
$ covenant verify TheRealDrainingContract.cov --proof CleanLookingBadge.json
   ✗ MISMATCH — the claimed proof does NOT correspond to this source:
       - contract: claimed CommunityToken, computed NetZeroDrain
       - safe: claimed true, computed false
   Do not trust the claimed badge.        # exit 1
```

If you only have the source and the **anchored** `proof_sha256` (e.g. read from
the on-chain registry that `covenant chainplan` targets), verify against the hash
directly — this is the seam between the rung-1 language guarantee and rung-2
anchoring:

```sh
covenant verify CommunityToken.cov --proof-hash 88b91b763b42a90d…
covenant verify CommunityToken.cov --proof badge.json --json   # machine-readable verdict
```

## How it works

A contract is an explicit, governed, audited **state machine over money-typed state** with **total value conservation** and **atomic settlement**. The pipeline:

```
.cov ─grammar→ CST ─lower→ IR ─check→ {diagnostics, rug-surface report}
                              └─interp→ atomic execution → deterministic receipt
```

- **Conservation is proven by capability deltas, not author-written holder loops.** A keyed `ledger` exposes only structured ops (`move`/`credit`/`debit`); the compiler proves each action stays in its declared lane, so value can't be created, destroyed, or reassigned through hidden `credit`/`debit` tricks except through a declared, **capped + quorum-gated** capability. The v1 interpreter also does a defensive in-memory invariant recheck before commit.
- **Governance policies are reusable.** A mint can use a named policy, a timelock, and an emission schedule:

```
policy council = approval 2 of { founder, treasurer, community } after 7 days

mint issue cap 1_000_000 TOKEN
    rate 100_000 TOKEN per 30 days
    by council {
    credit recipient : amount
}
```

The compiler resolves the policy into the rug-surface proof and the runtime rejects `MINT_TIMELOCK_PENDING` or `MINT_RATE_EXCEEDED` before touching state.
- **Atomic & deterministic runtime.** Every transition runs on a state snapshot: authority → quorum → cap → apply → re-verify conservation → commit-or-revert. No wall-clock, no I/O; the same inputs always yield the same byte-identical receipt.
- **The badge never lies.** An unsafe contract renders `⚠ UNSAFE`, not a clean check.

## Quickstart

```sh
go build ./cmd/covenant
covenant check       examples/community_token.cov
covenant rugsurface  examples/community_token.cov
covenant explain     examples/community_token.cov
covenant chainplan   examples/community_token.cov --target=evm-sepolia
covenant verify      examples/community_token.cov --proof badge.json
covenant run         examples/community_token.cov issue recipient=alice amount=500000 \
                       --caller=founder --now=1 --approvals=founder,treasurer
```

Run the tests (the thesis is a regression gate in `acceptance_test.go`):

```sh
go test ./...
```

> **Build note:** v1 pins the published [`gotreesitter`](https://github.com/odvcencio/gotreesitter) `v0.20.2` — no `replace` directive, so `go build ./...` works from a clean external checkout.

## Status — v1 (tracer bullet)

A safe mintable community token end-to-end: grammar → IR → conservation + mint-safety checks + rug-surface report → atomic interpreter + receipts → CLI. Pure-Go, cross-compiles to `wasip1/wasm` and `linux/arm64`.

**Scope:** single-instance, trusted-operator tokens; the guarantee is a **language-level** one (you cannot *author* a quiet rug). It is **not** trustless yet, and not DeFi.

## Roadmap

The guarantee strengthens up an enforcement ladder:

1. **Language (here, v1)** — you cannot *author* a quiet rug; clean contracts are provably power-free.
2. **+ Anchoring** — operator-signed, hash-chained receipts anchored to an external ledger → rugs become *detectable and attributable*.
3. **+ Verifiable execution** — a chain / ZK-proven VM / replicated runtime makes the guarantee *enforced at runtime*, even against a dishonest operator. This is where "legitimize all crypto" lands.

## Chain + testnet story

Covenant is chain-agnostic at the language layer. The first chain integration is
**anchor-only**: publish the Covenant source hash, rug-surface proof hash, and
receipt-chain heads to a minimal registry contract/program. That makes the v1
language guarantee public and auditable without pretending the chain is already
executing Covenant.

```sh
covenant chainplan examples/policy_token.cov --target=evm-sepolia
covenant chainplan examples/policy_token.cov --target=solana-devnet --json
```

Current target defaults:

- `local-evm`: fast local registry iteration before any public testnet.
- `evm-sepolia`: default public EVM app/testnet target.
- `evm-hoodi`: Ethereum validator/protocol-infra rehearsal, not default app testing.
- `starknet-sepolia`: Cairo/ZK-oriented proof registry experiments.
- `solana-devnet`: Solana app/wallet/explorer proof registry target.
- `solana-testnet`: Solana validator/stress testing after Devnet works.

Also queued: a bytecode VM (with the current interpreter as the differential-proof oracle); the agreement/state-machine shape (escrow, vesting, auctions) with governed gates and migration; exact-decimal money types; and a published verifier for the canonical rug-surface JSON proof.
