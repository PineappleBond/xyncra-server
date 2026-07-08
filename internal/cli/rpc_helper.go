package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"time"

	"github.com/gorilla/websocket"

	"github.com/PineappleBond/xyncra-server/pkg/client"
	"github.com/PineappleBond/xyncra-server/pkg/protocol"
)

// standaloneRPC sends a single JSON-RPC request over a fresh WebSocket
// connection and returns the raw response data (resp.Data from the server).
// This is the shared fallback for all RPC-backed CLI commands when the
// daemon IPC is unavailable (D-032).
func standaloneRPC(ctx context.Context, cliCtx *CLIContext, method string, params map[string]any) (json.RawMessage, error) {
	dialCtx, dialCancel := context.WithTimeout(ctx, 5*time.Second)
	defer dialCancel()

	url := cliCtx.ServerURLWithUser()
	ws, _, err := websocket.DefaultDialer.DialContext(dialCtx, url, nil)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", cliCtx.ServerURL, err)
	}
	defer ws.Close()

	paramsBytes, err := json.Marshal(params)
	if err != nil {
		return nil, fmt.Errorf("standalone marshal params: %w", err)
	}

	reqData, err := json.Marshal(protocol.PackageDataRequest{
		ID:     "1",
		Method: method,
		Params: json.RawMessage(paramsBytes),
	})
	if err != nil {
		return nil, fmt.Errorf("standalone marshal request: %w", err)
	}

	pkg := protocol.Package{
		Version: 1,
		Type:    protocol.PackageTypeRequest,
		Data:    json.RawMessage(reqData),
	}

	if err := ws.SetWriteDeadline(time.Now().Add(5 * time.Second)); err != nil {
		return nil, fmt.Errorf("standalone set write deadline: %w", err)
	}
	if err := ws.WriteJSON(pkg); err != nil {
		return nil, fmt.Errorf("standalone write: %w", err)
	}

	if err := ws.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		return nil, fmt.Errorf("standalone set read deadline: %w", err)
	}
	var respPkg protocol.Package
	if err := ws.ReadJSON(&respPkg); err != nil {
		var opErr net.Error
		if errors.As(err, &opErr) && opErr.Timeout() {
			return nil, fmt.Errorf("standalone read: server timed out")
		}
		return nil, fmt.Errorf("standalone read: %w", err)
	}

	var resp protocol.PackageDataResponse
	if err := json.Unmarshal(respPkg.Data, &resp); err != nil {
		return nil, fmt.Errorf("standalone unmarshal response: %w", err)
	}

	if resp.Code != protocol.ResponseCodeOK {
		return nil, &client.ClientError{Code: resp.Code, Message: resp.Msg}
	}

	return resp.Data, nil
}
