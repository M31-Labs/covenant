package chain

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"m31labs.dev/covenant/check"
)

type Target struct {
	ID          string `json:"id"`
	Stack       string `json:"stack"`
	Network     string `json:"network"`
	Use         string `json:"use"`
	AnchorModel string `json:"anchor_model"`
	Notes       string `json:"notes,omitempty"`
}

type Plan struct {
	Schema       string   `json:"schema"`
	Target       Target   `json:"target"`
	Contract     string   `json:"contract"`
	SourceSHA256 string   `json:"source_sha256"`
	ProofSHA256  string   `json:"proof_sha256"`
	Safe         bool     `json:"safe"`
	Mode         string   `json:"mode"`
	NextSteps    []string `json:"next_steps"`
}

func Targets() []Target {
	return []Target{
		{
			ID:          "local-evm",
			Stack:       "EVM",
			Network:     "local devnet",
			Use:         "fast adapter and registry-contract iteration before any public testnet",
			AnchorModel: "emit proof and receipt hashes from a minimal registry contract",
			Notes:       "use Foundry/Anvil or an equivalent local EVM node",
		},
		{
			ID:          "evm-sepolia",
			Stack:       "EVM",
			Network:     "Sepolia",
			Use:         "default public testnet for application, contract, and wallet integration",
			AnchorModel: "registry event: source hash, rug-surface proof hash, receipt hash chain",
		},
		{
			ID:          "evm-hoodi",
			Stack:       "EVM",
			Network:     "Hoodi",
			Use:         "validator, staking, and protocol-infrastructure integration only",
			AnchorModel: "same registry event model as Sepolia, used for infra rehearsals",
		},
		{
			ID:          "starknet-sepolia",
			Stack:       "Starknet/Cairo",
			Network:     "Starknet Sepolia",
			Use:         "Cairo/ZK-oriented verifier and L2 proof registry experiments",
			AnchorModel: "contract stores proof hash and receipt chain head; future Cairo verifier consumes Covenant bytecode proof",
		},
		{
			ID:          "solana-devnet",
			Stack:       "Solana",
			Network:     "Devnet",
			Use:         "application-level proof registry and wallet/explorer integration",
			AnchorModel: "program log/account records source hash, proof hash, and receipt chain head",
		},
		{
			ID:          "solana-testnet",
			Stack:       "Solana",
			Network:     "Testnet",
			Use:         "validator and stress-testing only after Devnet adapter works",
			AnchorModel: "same proof registry model as Devnet, used under less stable network conditions",
		},
	}
}

func TargetByID(id string) (Target, bool) {
	for _, t := range Targets() {
		if t.ID == id {
			return t, true
		}
	}
	return Target{}, false
}

func Build(targetID string, source []byte, report check.RugSurfaceReport) (Plan, error) {
	target, ok := TargetByID(targetID)
	if !ok {
		return Plan{}, fmt.Errorf("unknown chain target %q", targetID)
	}
	sourceSum := sha256.Sum256(source)
	proofHash, err := report.ProofHash()
	if err != nil {
		return Plan{}, err
	}
	return Plan{
		Schema:       "m31labs.covenant.chain_plan.v1",
		Target:       target,
		Contract:     report.Contract,
		SourceSHA256: hex.EncodeToString(sourceSum[:]),
		ProofSHA256:  proofHash,
		Safe:         report.Safe(),
		Mode:         "anchor-only",
		NextSteps:    nextSteps(target),
	}, nil
}

func (p Plan) JSON() (string, error) {
	b, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return "", err
	}
	return string(b) + "\n", nil
}

func (p Plan) Text() string {
	var b strings.Builder
	fmt.Fprintf(&b, "Covenant chain plan — %s on %s\n\n", p.Contract, p.Target.ID)
	fmt.Fprintf(&b, "stack:  %s\n", p.Target.Stack)
	fmt.Fprintf(&b, "network: %s\n", p.Target.Network)
	fmt.Fprintf(&b, "mode:   %s\n", p.Mode)
	fmt.Fprintf(&b, "use:    %s\n", p.Target.Use)
	fmt.Fprintf(&b, "anchor: %s\n", p.Target.AnchorModel)
	if p.Target.Notes != "" {
		fmt.Fprintf(&b, "notes:  %s\n", p.Target.Notes)
	}
	fmt.Fprintf(&b, "\nsource sha256: %s\n", p.SourceSHA256)
	fmt.Fprintf(&b, "proof  sha256: %s\n", p.ProofSHA256)
	if p.Safe {
		b.WriteString("status: SAFE to anchor as a v1 language-level proof\n")
	} else {
		b.WriteString("status: UNSAFE; fix diagnostics before anchoring publicly\n")
	}
	b.WriteString("\nnext steps:\n")
	for _, step := range p.NextSteps {
		fmt.Fprintf(&b, "- %s\n", step)
	}
	return b.String()
}

func nextSteps(target Target) []string {
	switch target.ID {
	case "evm-sepolia":
		return []string{
			"deploy the minimal CovenantAnchor registry contract to Sepolia",
			"submit source_sha256 and proof_sha256 as an AnchorProof event",
			"append each deterministic receipt hash as the next receipt-chain head",
		}
	case "evm-hoodi":
		return []string{
			"use only after the Sepolia adapter is stable",
			"rehearse validator/staking-infra workflows and registry availability",
			"do not treat Hoodi as the app/default dapp testnet",
		}
	case "starknet-sepolia":
		return []string{
			"deploy a Cairo proof registry contract",
			"anchor source_sha256 and proof_sha256 on Starknet Sepolia",
			"prototype a future Cairo verifier for Covenant bytecode receipts",
		}
	case "solana-devnet":
		return []string{
			"deploy a tiny CovenantAnchor program on Devnet",
			"record proof hashes in program logs or a proof account",
			"test wallet/explorer display of the rug-surface proof",
		}
	case "solana-testnet":
		return []string{
			"use after Devnet proof registry works",
			"stress-test proof anchoring under validator/testnet conditions",
			"expect intermittent resets and avoid product demos here",
		}
	default:
		return []string{
			"iterate locally until proof/receipt anchoring is deterministic",
			"then graduate to evm-sepolia or solana-devnet for public app testing",
		}
	}
}
