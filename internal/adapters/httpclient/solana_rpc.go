package httpclient

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"strings"

	"github.com/SourceSenseiTheRealOne/solana-market-research/internal/domain"
	"github.com/SourceSenseiTheRealOne/solana-market-research/internal/ports"
)

const (
	legacyTokenProgram = "TokenkegQfeZyiNwAJbNbGKPFXCWuBvf9Ss623VQ5DA"
	token2022Program   = "TokenzQdBNbLqP5VEhdkAS6EPFLC1PHnBqCXEpPxuEb"
	mintBaseLength     = 82
)

type SolanaRPC struct{ client *Client }

func NewSolanaRPC(client *Client) *SolanaRPC { return &SolanaRPC{client: client} }

func (rpc *SolanaRPC) Inspect(ctx context.Context, mint string) (domain.TokenRiskSnapshot, error) {
	if rpc.client == nil || strings.TrimSpace(mint) == "" {
		return domain.TokenRiskSnapshot{}, errors.New("Solana RPC inspector requires a client and mint address")
	}
	var response rpcAccountInfoResponse
	request := rpcRequest{JSONRPC: "2.0", ID: 1, Method: "getAccountInfo", Params: []any{mint, map[string]string{"commitment": "finalized", "encoding": "base64"}}}
	if err := rpc.client.PostJSON(ctx, "/", request, &response); err != nil {
		return domain.TokenRiskSnapshot{}, fmt.Errorf("fetch Solana mint account: %w", err)
	}
	if response.Error != nil || response.Result.Value == nil {
		return domain.TokenRiskSnapshot{}, errors.New("Solana RPC returned no mint account")
	}
	return decodeMintAccount(response.Result.Value.Owner, response.Result.Value.Data)
}

type rpcRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int    `json:"id"`
	Method  string `json:"method"`
	Params  []any  `json:"params"`
}

type rpcAccountInfoResponse struct {
	Error  any `json:"error"`
	Result struct {
		Value *struct {
			Owner string   `json:"owner"`
			Data  []string `json:"data"`
		} `json:"value"`
	} `json:"result"`
}

func decodeMintAccount(owner string, encodedData []string) (domain.TokenRiskSnapshot, error) {
	program, ok := tokenProgram(owner)
	if !ok {
		return domain.TokenRiskSnapshot{}, errors.New("mint account is not owned by a supported token program")
	}
	if len(encodedData) != 2 || encodedData[1] != "base64" {
		return domain.TokenRiskSnapshot{}, errors.New("mint account data is not base64 encoded")
	}
	data, err := base64.StdEncoding.DecodeString(encodedData[0])
	if err != nil || len(data) < mintBaseLength {
		return domain.TokenRiskSnapshot{}, errors.New("mint account data is malformed")
	}
	mintAuthorityRevoked, err := cOptionAbsent(data[0:4])
	if err != nil {
		return domain.TokenRiskSnapshot{}, fmt.Errorf("decode mint authority: %w", err)
	}
	freezeAuthorityRevoked, err := cOptionAbsent(data[46:50])
	if err != nil {
		return domain.TokenRiskSnapshot{}, fmt.Errorf("decode freeze authority: %w", err)
	}
	snapshot := domain.TokenRiskSnapshot{Program: program, MintAuthorityRevoked: mintAuthorityRevoked, FreezeAuthorityRevoked: freezeAuthorityRevoked}
	if program == domain.TokenProgram2022 {
		snapshot.Extensions, err = decodeToken2022Extensions(data[mintBaseLength:])
		if err != nil {
			return domain.TokenRiskSnapshot{}, err
		}
	}
	return snapshot, nil
}

func tokenProgram(owner string) (domain.TokenProgram, bool) {
	switch owner {
	case legacyTokenProgram:
		return domain.TokenProgramLegacy, true
	case token2022Program:
		return domain.TokenProgram2022, true
	default:
		return "", false
	}
}

func cOptionAbsent(raw []byte) (bool, error) {
	if len(raw) != 4 {
		return false, errors.New("invalid COption tag length")
	}
	switch binary.LittleEndian.Uint32(raw) {
	case 0:
		return true, nil
	case 1:
		return false, nil
	default:
		return false, errors.New("invalid COption tag")
	}
}

func decodeToken2022Extensions(data []byte) ([]domain.TokenExtension, error) {
	extensions := make([]domain.TokenExtension, 0)
	for len(data) > 0 {
		if len(data) < 4 {
			return nil, errors.New("malformed Token-2022 extension header")
		}
		typeID := binary.LittleEndian.Uint16(data[:2])
		length := int(binary.LittleEndian.Uint16(data[2:4]))
		if typeID == 0 && length == 0 {
			return extensions, nil
		}
		if len(data) < 4+length {
			return nil, errors.New("malformed Token-2022 extension payload")
		}
		extensions = append(extensions, token2022ExtensionName(typeID))
		data = data[4+length:]
	}
	return extensions, nil
}

func token2022ExtensionName(typeID uint16) domain.TokenExtension {
	switch typeID {
	case 1:
		return "TransferFeeConfig"
	case 3:
		return "MintCloseAuthority"
	case 6:
		return "DefaultAccountState"
	case 9:
		return "NonTransferable"
	case 10:
		return "InterestBearingConfig"
	case 12:
		return "PermanentDelegate"
	case 14:
		return "TransferHook"
	default:
		return domain.TokenExtension(fmt.Sprintf("type_%d", typeID))
	}
}

var _ ports.TokenInspector = (*SolanaRPC)(nil)
