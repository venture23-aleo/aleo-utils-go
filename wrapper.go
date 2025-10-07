// Package aleo_utils implements Aleo-compatible Schnorr signing.
package aleo_utils

import (
	"context"
	"crypto/rand"
	_ "embed"
	"errors"
	"fmt"
	"log"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
	"github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"
)

//go:embed aleo_utils.wasm
var wasmBytes []byte

var ErrNoRuntime = errors.New("no runtime, create new wrapper")

const (
	PRIVATE_KEY_SIZE          = 59
	ADDRESS_SIZE              = 63
	SIGNATURE_SIZE            = 216
	MESSAGE_FORMAT_BLOCK_SIZE = 16 * 32
	MAX_FORMAT_MESSAGE_CHUNKS = 32
)

// Wrapper is an interface for Aleo Wrapper session manager. Create an instance of a Wrapper using
// NewWrapper, then create a new Session to use the signing functionality.
type Wrapper interface {
	NewSession() (Session, error)
	Close()
}

func logString(ctx context.Context, module api.Module, ptr, byteCount uint32) {
	buf, ok := module.Memory().Read(ptr, byteCount)
	if ok {
		log.Println("Aleo Wrapper log:", string(buf))
	}
}

type aleoWrapper struct {
	Wrapper

	runtime       wazero.Runtime
	cmod          wazero.CompiledModule
	moduleConfig  wazero.ModuleConfig
	runtimeActive bool // a simple guard against using wrapper after it's runtime was destroyed
}

// NewWrapper creates Leo contract compatible Schnorr wrapper manager.
// The second argument is a cleanup function, which destroys wrapper runtime.
// aleoWrapper cannot be used after the cleanup function is called, and must be recreated using this function.
func NewWrapper() (wrapper Wrapper, closeFn func(), err error) {
	defer func() {
		if r := recover(); r != nil {
			// find out exactly what the error was and set err
			switch x := r.(type) {
			case string:
				err = errors.New(x)
			case error:
				err = x
			default:
				err = errors.New("unknown panic")
			}
			wrapper = nil
			closeFn = func() {}
		}
	}()

	ctx := context.Background()

	runtimeConfig := wazero.NewRuntimeConfigCompiler()
	runtime := wazero.NewRuntimeWithConfig(ctx, runtimeConfig)
	// If we fail later in this function, make sure to close the runtime to avoid leaks.
	defer func() {
		if err != nil && runtime != nil {
			_ = runtime.Close(ctx)
		}
	}()

	// export some wasi system functions
	wasi_snapshot_preview1.MustInstantiate(ctx, runtime)

	// export logging function to the guest
	hostBuilder := runtime.NewHostModuleBuilder("env")
	if _, hbErr := hostBuilder.NewFunctionBuilder().WithFunc(logString).Export("host_log_string").Instantiate(ctx); hbErr != nil {
		return nil, nil, fmt.Errorf("failed to instantiate host module: %w", hbErr)
	}

	moduleConfig := wazero.NewModuleConfig().WithRandSource(rand.Reader)

	cmod, err := runtime.CompileModule(ctx, wasmBytes)
	if err != nil {
		return nil, nil, err
	}
	log.Println("compiled wrapper WASM module")

	wrapper = &aleoWrapper{
		runtime:       runtime,
		cmod:          cmod,
		moduleConfig:  moduleConfig,
		runtimeActive: true,
	}

	return wrapper, wrapper.Close, nil
}

// NewSession creates a new wrapper session, which can used to access signing logic. Sessions
// are not goroutine-safe.
func (s *aleoWrapper) NewSession() (Session, error) {
	if !s.runtimeActive || s.runtime == nil {
		s.runtime = nil
		return nil, ErrNoRuntime
	}

	mod, err := s.runtime.InstantiateModule(context.Background(), s.cmod, s.moduleConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to instantiate wrapper session: %w", err)
	}

	// Resolve and validate all required exports explicitly.
	required := map[string]api.Function{
		"new_private_key":            mod.ExportedFunction("new_private_key"),
		"get_address":                mod.ExportedFunction("get_address"),
		"sign":                       mod.ExportedFunction("sign"),
		"alloc":                      mod.ExportedFunction("alloc"),
		"dealloc":                    mod.ExportedFunction("dealloc"),
		"hash_message":               mod.ExportedFunction("hash_message"),
		"hash_message_bytes":         mod.ExportedFunction("hash_message_bytes"),
		"format_message":             mod.ExportedFunction("format_message"),
		"formatted_message_to_bytes": mod.ExportedFunction("formatted_message_to_bytes"),
	}

	missing := make([]string, 0)
	for name, fn := range required {
		if fn == nil {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		// Ensure we don't leak a module instance if it's unusable
		_ = mod.Close(context.Background())
		return nil, fmt.Errorf("missing required wasm exports: %v", missing)
	}

	session := &aleoWrapperSession{
		mod:              mod,
		ctx:              context.Background(),
		newPrivateKey:    required["new_private_key"],
		getAddress:       required["get_address"],
		sign:             required["sign"],
		allocate:         required["alloc"],
		deallocate:       required["dealloc"],
		hashMessage:      required["hash_message"],
		hashMessageBytes: required["hash_message_bytes"],
		formatMessage:    required["format_message"],
		recoverMessage:   required["formatted_message_to_bytes"],
	}

	return session, nil
}

// Closes WASM runtime
func (s *aleoWrapper) Close() {
	if s.runtime != nil {
		s.runtime.Close(context.Background())
	}
	s.runtimeActive = false
}
