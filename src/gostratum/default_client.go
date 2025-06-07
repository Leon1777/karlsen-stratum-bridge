package gostratum

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/karlsen-network/karlsen-stratum-bridge/v2/src/utils"
	"github.com/karlsen-network/karlsend/v2/util"
	"github.com/mattn/go-colorable"
	"github.com/pkg/errors"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

type StratumMethod string

const (
	StratumMethodSubscribe StratumMethod = "mining.subscribe"
	StratumMethodAuthorize StratumMethod = "mining.authorize"
	StratumMethodSubmit    StratumMethod = "mining.submit"
)

func DefaultLogger() *zap.Logger {
	cfg := zap.NewDevelopmentEncoderConfig()
	cfg.EncodeLevel = zapcore.CapitalColorLevelEncoder
	return zap.New(zapcore.NewCore(
		zapcore.NewConsoleEncoder(cfg),
		&utils.BufferedWriteSyncer{WS: zapcore.AddSync(colorable.NewColorableStdout()), FlushInterval: 5 * time.Second},
		zapcore.DebugLevel,
	))
}

func DefaultConfig(logger *zap.Logger, testnetMining bool) StratumListenerConfig {
	return StratumListenerConfig{
		StateGenerator: func() any { return nil },
		HandlerMap:     DefaultHandlers(testnetMining),
		Port:           ":5555",
		Logger:         logger,
	}
}

func DefaultHandlers(testnetMining bool) StratumHandlerMap {
	return StratumHandlerMap{
		string(StratumMethodSubscribe): HandleSubscribe,
		string(StratumMethodAuthorize): func(ctx *StratumContext, event JsonRpcEvent) error {
			return HandleAuthorize(ctx, event, testnetMining)
		},
		string(StratumMethodSubmit): HandleSubmit,
	}
}

func HandleAuthorize(ctx *StratumContext, event JsonRpcEvent, testnetMining bool) error {
	if len(event.Params) < 1 {
		return fmt.Errorf("malformed event from miner, expected param[1] to be address")
	}
	address, ok := event.Params[0].(string)
	if !ok {
		return fmt.Errorf("malformed event from miner, expected param[1] to be address string")
	}
	parts := strings.Split(address, ".")
	var workerName string
	if len(parts) >= 2 {
		address = parts[0]
		workerName = parts[1]
	}

	address, err := CleanWallet(address, testnetMining)
	if err != nil {
		return fmt.Errorf("invalid wallet format %s: %w", address, err)
	}

	ctx.WalletAddr = address
	ctx.WorkerName = workerName
	ctx.Logger = ctx.Logger.With(zap.String("worker", ctx.WorkerName), zap.String("addr", ctx.WalletAddr))

	if err := ctx.Reply(NewResponse(event, true, nil)); err != nil {
		return errors.Wrap(err, "failed to send response to authorize")
	}
	if ctx.Extranonce != "" {
		SendExtranonce(ctx)
	}

	ctx.Logger.Info(fmt.Sprintf("client authorized, address: %s", ctx.WalletAddr))
	return nil
}

func HandleSubscribe(ctx *StratumContext, event JsonRpcEvent) error {
	if err := ctx.Reply(NewResponse(event,
		[]any{true, "EthereumStratum/1.0.0"}, nil)); err != nil {
		return errors.Wrap(err, "failed to send response to subscribe")
	}
	if len(event.Params) > 0 {
		app, ok := event.Params[0].(string)
		if ok {
			ctx.RemoteApp = app
		}
	}

	ctx.Logger.Info("client subscribed ", zap.Any("context", ctx))
	return nil
}

func HandleSubmit(ctx *StratumContext, event JsonRpcEvent) error {
	// stub
	ctx.Logger.Info("work submission")
	return nil
}

func SendExtranonce(ctx *StratumContext) {
	if err := ctx.Send(NewEvent("", "set_extranonce", []any{ctx.Extranonce})); err != nil {
		ctx.Logger.Error(errors.Wrap(err, "failed to set extranonce").Error(), zap.Any("context", ctx))
	}
}

// CleanWallet function handles both testnet and mainnet wallet addresses
func CleanWallet(in string, testnetMining bool) (string, error) {
	if testnetMining {
		// handle testnet wallet prefix and regex
		fmt.Printf("Switching to testnet address handling: %s\n", in)
		_, err := util.DecodeAddress(in, util.Bech32PrefixKarlsenTest)
		if err == nil {
			return in, nil // valid testnet address
		}
		if !strings.HasPrefix(in, "karlsentest:") {
			in = "karlsentest:" + in
		}
		// validate the address format using regex
		if regexp.MustCompile("^karlsentest:[a-z0-9]+$").MatchString(in) {
			return in[:73], nil
		}
	} else {
		// handle mainnet wallet prefix and regex
		fmt.Printf("Handling mainnet address: %s\n", in)
		_, err := util.DecodeAddress(in, util.Bech32PrefixKarlsen)
		if err == nil {
			return in, nil // valid mainnet address
		}
		if !strings.HasPrefix(in, "karlsen:") {
			in = "karlsen:" + in
		}
		// validate the address format using regex
		if regexp.MustCompile("^karlsen:[a-z0-9]+$").MatchString(in) {
			return in[:69], nil
		}
	}
	return "", errors.New("unable to coerce wallet to valid karlsen address")
}
