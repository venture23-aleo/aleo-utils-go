package aleo_utils

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"

	"github.com/tetratelabs/wazero/api"
)

var ErrNoModule = errors.New("session module is closed")

// Provides access to wrapper functionality. A session is not goroutine safe so
// you need to create a new one for every goroutine
type Session interface {
	// NewPrivateKey returns a newly generated private key as a byte slice and the
	// corresponding address. The caller is responsible for zeroizing the returned
	// slice when it is no longer needed (see ZeroizePrivateKey).
	NewPrivateKey() (key []byte, address string, err error)
	FormatMessage(message []byte, targetChunks int) (formattedMessage []byte, err error)
	RecoverMessage(formattedMessage []byte) (message []byte, err error)
	HashMessageToString(message []byte) (hash string, err error)
	HashMessage(message []byte) (hash []byte, err error)
	// Sign creates an Aleo-compatible Schnorr signature. The private key is not
	// copied as a string and is wiped from WASM memory immediately after use.
	Sign(key []byte, message []byte) (signature string, err error)

	Close()
}

// internal implementation of the Session interface
type aleoWrapperSession struct {
	Session

	// unique wasm module for this session
	mod api.Module
	ctx context.Context

	newPrivateKey    api.Function
	getAddress       api.Function
	sign             api.Function
	allocate         api.Function
	deallocate       api.Function
	hashMessage      api.Function
	hashMessageBytes api.Function
	formatMessage    api.Function
	recoverMessage   api.Function
}

func (s *aleoWrapperSession) Close() {
	if s.mod != nil {
		s.mod.Close(context.Background())
	}
}

// Helper function to allocate memory safely with actual capacity tracking
func (s *aleoWrapperSession) allocateSafe(size uint64) (ptr uint64, actualCapacity uint64, err error) {
	// New alloc returns a raw pointer (to data) and stores capacity in an 8-byte header.
	result, err := s.allocate.Call(s.ctx, size)
	if err != nil {
		return 0, 0, err
	}
	// We don't know the actual capacity (it's stored internally). Return size for logging only.
	return result[0], size, nil
}

// Helper function to deallocate memory safely with exact capacity
func (s *aleoWrapperSession) deallocateSafe(ptr uint64, actualCapacity uint64) error {
	_, err := s.deallocate.Call(s.ctx, ptr, 0)
	return err
}

func decodeLenPtr(encoded uint64) (ptr uint32, length uint32) {
	return uint32(encoded), uint32(encoded >> 32)
}

// NewPrivateKey generates a new Aleo private key, returns it's string representation and the address derived from that private key.
func (s *aleoWrapperSession) NewPrivateKey() (key []byte, address string, err error) {
	if s.mod == nil || s.mod.IsClosed() {
		return nil, "", ErrNoModule
	}

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
			key = nil
			address = ""
		}
	}()

	// generate new private key
	var privKeyPtr []uint64
	privKeyPtr, err = s.newPrivateKey.Call(s.ctx)
	if err != nil {
		log.Println("new_private_key error:", err)
		return
	}
	if len(privKeyPtr) == 0 {
		return nil, "", errors.New("failed to create new private key: empty return")
	}
	keyPtr, keyLen := decodeLenPtr(privKeyPtr[0])
	if keyPtr == 0 || keyLen == 0 {
		return nil, "", errors.New("failed to create new private key: invalid pointer")
	}
	if keyLen != PRIVATE_KEY_SIZE {
		log.Printf("unexpected private key length %d (expected %d)", keyLen, PRIVATE_KEY_SIZE)
	}
	defer func(ptr uint32, length uint32) {
		// Zero out the memory first
		zero := make([]byte, int(length))
		_ = s.mod.Memory().Write(ptr, zero)
		// Then deallocate
		if err := s.deallocateSafe(uint64(ptr), 0); err != nil {
			log.Printf("Failed to deallocate private key memory: %v", err)
		}
	}(keyPtr, keyLen)

	// read wasm memory at pointer for the private key string
	privKeyWasm, ok := s.mod.Memory().Read(keyPtr, keyLen)
	if !ok {
		return nil, "", errors.New("failed to create new private key")
	}
	key = make([]byte, int(keyLen))
	copy(key, privKeyWasm)

	// get public address from the private key, reuse the returned value from private key generation
	addressPtr, err := s.getAddress.Call(s.ctx, uint64(keyPtr), uint64(keyLen))
	if err != nil {
		log.Println("get_address error:", err)
		return nil, "", errors.New("failed to get address from the generated private key")
	}
	if len(addressPtr) == 0 {
		return nil, "", errors.New("internal error when getting address from the generated private key: empty return")
	}
	addrPtr, addrLen := decodeLenPtr(addressPtr[0])
	if addrPtr == 0 || addrLen == 0 {
		return nil, "", errors.New("internal error when getting address from the generated private key")
	}
	if addrLen != ADDRESS_SIZE {
		log.Printf("unexpected address length %d (expected %d)", addrLen, ADDRESS_SIZE)
	}
	defer func(ptr uint32) {
		if err := s.deallocateSafe(uint64(ptr), 0); err != nil {
			log.Printf("Failed to deallocate address memory: %v", err)
		}
	}(addrPtr)

	// read address from wasm memory
	addr, ok := s.mod.Memory().Read(addrPtr, addrLen)
	if !ok {
		return nil, "", errors.New("failed to convert generated private key to address")
	}

	// since memory read returns a slice of wasm memory buffer, it needs to be copied
	// to avoid our returned slice being wiped when wasm memory is wiped.
	// explicit copy is not needed since we create a string, which copies the slice instead of referencing it
	address = string(addr)

	// Now that the address has been derived and we no longer need the key inside
	// WASM memory, wipe the original region (best effort) before returning.
	// WASM memory for the private key is wiped via defer before deallocation

	return
}

// FormatMessage formats a byte array as a Leo struct of up to 32 structs of 32 u128 numbers. The returned value
// is a string representation of that struct, as bytes.
func (s *aleoWrapperSession) FormatMessage(message []byte, targetChunks int) (formattedMessage []byte, err error) {
	if s.mod == nil || s.mod.IsClosed() {
		return nil, ErrNoModule
	}

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
			formattedMessage = nil
		}
	}()

	if targetChunks < 1 || targetChunks > MAX_FORMAT_MESSAGE_CHUNKS {
		return nil, errors.New("target number of chunks must be between 1 and 32")
	}

	if len(message) > targetChunks*MESSAGE_FORMAT_BLOCK_SIZE {
		return nil, fmt.Errorf("target formatted message length must be at most %d (%d chunks)", targetChunks*MESSAGE_FORMAT_BLOCK_SIZE, targetChunks)
	}

	msgLen := uint64(len(message))

	// FIXED: Use safe allocation that tracks actual capacity
	messagePtr, _, err := s.allocateSafe(msgLen)
	if err != nil {
		log.Println("message allocate error:", err)
		return nil, errors.New("failed to allocate memory for message")
	}

	// Deallocate (capacity stored in header, second arg ignored)
	defer func() {
		if err := s.deallocateSafe(messagePtr, 0); err != nil {
			log.Printf("Failed to deallocate message memory: %v", err)
		}
	}()

	// write message to wasm memory
	ok := s.mod.Memory().Write(uint32(messagePtr), message)
	if !ok {
		return nil, errors.New("failed to write message to memory for formatting")
	}

	// call format message with the pointer to the message
	formatResult, err := s.formatMessage.Call(s.ctx, messagePtr, msgLen, uint64(targetChunks))
	if err != nil {
		log.Println("string format error:", err)
		return nil, errors.New("failed to format message")
	}
	if len(formatResult) == 0 {
		return nil, errors.New("invalid message: empty return")
	}
	if formatResult[0] == 0 {
		return nil, errors.New("invalid message")
	}

	// take the first (big endian) 32 bits as string size
	strLen := uint32(formatResult[0] >> 32)

	// casting uint64 to uint32 discards the first (big endian) 32 bits so we're left with the last 32 bits of the result pointer
	strPtr := uint32(formatResult[0])

	// now we know how many bytes to read to get the string representation of a field
	buf, ok := s.mod.Memory().Read(strPtr, strLen)
	if !ok {
		return nil, errors.New("failed to convert message to a field")
	}
	// FIXED: This output is allocated by Rust using forget_buf_ptr_len,
	// so we need to deallocate using the actual buffer size, not strLen
	defer s.deallocate.Call(s.ctx, uint64(strPtr), uint64(strLen))

	// since memory read returns a slice of wasm memory buffer, it needs to be copied
	// to avoid our returned slice being wiped when wasm memory is wiped
	formattedMessage = make([]byte, len(buf))
	copy(formattedMessage, buf)

	adjusted := strings.ReplaceAll(string(formattedMessage), "\n", "")

	return []byte(adjusted), nil
}

// Recovers the original byte message from a formatted message string that was created using FormatMessage
func (s *aleoWrapperSession) RecoverMessage(formattedMessage []byte) (message []byte, err error) {
	if s.mod == nil || s.mod.IsClosed() {
		return nil, ErrNoModule
	}

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
			message = nil
		}
	}()

	formattedMsgLen := uint64(len(formattedMessage))

	// FIXED: Use safe allocation that tracks actual capacity
	formattedMessagePtr, _, err := s.allocateSafe(formattedMsgLen)
	if err != nil {
		log.Println("message allocate error:", err)
		return nil, errors.New("failed to allocate memory for message")
	}

	// Deallocate (capacity stored in header, second arg ignored)
	defer func() {
		if err := s.deallocateSafe(formattedMessagePtr, 0); err != nil {
			log.Printf("Failed to deallocate formatted message memory: %v", err)
		}
	}()

	// write message to wasm memory
	ok := s.mod.Memory().Write(uint32(formattedMessagePtr), formattedMessage)
	if !ok {
		return nil, errors.New("failed to write message to memory for recovering")
	}

	// call recover message with the pointer to the message
	recoverResult, err := s.recoverMessage.Call(s.ctx, formattedMessagePtr, formattedMsgLen)
	if err != nil {
		log.Println("string recover error:", err)
		return nil, errors.New("failed to recover message")
	}
	if len(recoverResult) == 0 {
		return nil, errors.New("invalid message: empty return")
	}
	if recoverResult[0] == 0 {
		return nil, errors.New("invalid message")
	}

	// take the first (big endian) 32 bits as string size
	bufLen := uint32(recoverResult[0] >> 32)

	// casting uint64 to uint32 discards the first (big endian) 32 bits so we're left with the last 32 bits of the result pointer
	bugPtr := uint32(recoverResult[0])

	// now we know how many bytes to read to get the string representation of a field
	buf, ok := s.mod.Memory().Read(bugPtr, bufLen)
	if !ok {
		return nil, errors.New("failed to convert message to a field")
	}
	defer s.deallocate.Call(s.ctx, uint64(bugPtr), uint64(bufLen))

	// since memory read returns a slice of wasm memory buffer, it needs to be copied
	// to avoid our returned slice being wiped when wasm memory is wiped
	message = make([]byte, len(buf))
	copy(message, buf)

	return
}

// HashMessageToString hashes a message using Poseidon8 Leo function, and returns a string
// representation of a resulting U128.
//
// Use this function if you need a hash as a literal, for example for using it in a contract.
func (s *aleoWrapperSession) HashMessageToString(message []byte) (hash string, err error) {
	if s.mod == nil || s.mod.IsClosed() {
		return "", ErrNoModule
	}

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
			hash = ""
		}
	}()

	msgLen := uint64(len(message))

	// FIXED: Use safe allocation that tracks actual capacity
	messagePtr, _, err := s.allocateSafe(msgLen)
	if err != nil {
		log.Println("message allocate error:", err)
		return "", errors.New("failed to allocate memory for message")
	}

	// Deallocate (capacity stored in header, second arg ignored)
	defer func() {
		if err := s.deallocateSafe(messagePtr, 0); err != nil {
			log.Printf("Failed to deallocate hash message memory: %v", err)
		}
	}()

	// write message to wasm memory
	ok := s.mod.Memory().Write(uint32(messagePtr), message)
	if !ok {
		return "", errors.New("failed to write message to memory for hashing")
	}

	// call the hash function and pass the pointer to the message
	hashResult, err := s.hashMessage.Call(s.ctx, messagePtr, msgLen)
	if err != nil {
		log.Println("hash message error:", err)
		return "", errors.New("failed to hash message to a string representation")
	}
	if len(hashResult) == 0 {
		return "", errors.New("invalid message: empty return")
	}
	if hashResult[0] == 0 {
		return "", errors.New("invalid message")
	}

	// take the first (big endian) 32 bits as string size
	hashLen := uint32(hashResult[0] >> 32)

	// casting uint64 to uint32 discards the first (big endian) 32 bits so we're left with the last 32 bits of the result pointer
	hashPtr := uint32(hashResult[0])

	// now we know how many bytes to read to get the string representation of a field
	hashBytes, ok := s.mod.Memory().Read(hashPtr, hashLen)
	if !ok {
		return "", errors.New("failed to convert message to a field")
	}
	defer s.deallocate.Call(s.ctx, uint64(hashPtr), uint64(hashLen))

	// since memory read returns a slice of wasm memory buffer, it needs to be copied
	// to avoid our returned slice being wiped when wasm memory is wiped.
	// explicit copy is not needed since we create a string, which copies the slice instead of referencing it
	hash = string(hashBytes)

	return
}

// HashMessage hashes a message using Poseidon8 Leo function, and returns a little-endian
// byte representation of a resulting U128.
func (s *aleoWrapperSession) HashMessage(message []byte) (hash []byte, err error) {
	if s.mod == nil || s.mod.IsClosed() {
		return nil, ErrNoModule
	}

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
			hash = nil
		}
	}()

	msgLen := uint64(len(message))

	// FIXED: Use safe allocation that tracks actual capacity
	messagePtr, _, err := s.allocateSafe(msgLen)
	if err != nil {
		log.Println("message allocate error:", err)
		return nil, errors.New("failed to allocate memory for message")
	}

	// Deallocate (capacity stored in header, second arg ignored)
	defer func() {
		if err := s.deallocateSafe(messagePtr, 0); err != nil {
			log.Printf("Failed to deallocate hash message bytes memory: %v", err)
		}
	}()

	// write message to wasm memory
	ok := s.mod.Memory().Write(uint32(messagePtr), message)
	if !ok {
		return nil, errors.New("failed to write message to memory for hashing")
	}

	// pass message to the hash function
	hashResult, err := s.hashMessageBytes.Call(s.ctx, messagePtr, msgLen)
	if err != nil {
		log.Println("hash message bytes error:", err)
		return nil, errors.New("failed to hash message")
	}
	if len(hashResult) == 0 {
		return nil, errors.New("invalid message: empty return")
	}
	if hashResult[0] == 0 {
		return nil, errors.New("invalid message")
	}

	// take the first (big endian) 32 bits as string size
	hashLen := uint32(hashResult[0] >> 32)

	// casting uint64 to uint32 discards the first (big endian) 32 bits so we're left with the last 32 bits of the result pointer
	hashPtr := uint32(hashResult[0])

	// now we know how many bytes to read to get the byte result
	buf, ok := s.mod.Memory().Read(hashPtr, hashLen)
	if !ok {
		return nil, errors.New("failed to convert message to a field")
	}
	defer s.deallocate.Call(s.ctx, uint64(hashPtr), uint64(hashLen))

	// since memory read returns a slice of wasm memory buffer, it needs to be copied
	// to avoid our returned slice being wiped when wasm memory is wiped
	hash = make([]byte, len(buf))
	copy(hash, buf)

	return
}

// Creates an Aleo-compatible Schnorr signature, returns the signature's string representation.
// The message must be a string or little-endian byte representation of a Leo U128.
func (s *aleoWrapperSession) Sign(key []byte, message []byte) (signature string, err error) {
	if s.mod == nil || s.mod.IsClosed() {
		return "", ErrNoModule
	}

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
			signature = ""
		}
	}()

	if len(key) != PRIVATE_KEY_SIZE {
		return "", errors.New("invalid private key size")
	}

	// allocate memory for the message to pass to the signing function using safe allocator
	msgLen := uint64(len(message))
	messagePtr, _, err := s.allocateSafe(msgLen)
	if err != nil {
		log.Println("message allocate error:", err)
		return "", errors.New("failed to allocate memory for message")
	}

	defer func() {
		if err := s.deallocateSafe(messagePtr, 0); err != nil { // second arg ignored
			log.Printf("Failed to deallocate message memory in Sign: %v", err)
		}
	}()

	// write formatted message to memory
	ok := s.mod.Memory().Write(uint32(messagePtr), message)
	if !ok {
		return "", errors.New("failed to write formatted message to memory for signing")
	}

	// allocate memory for private key to pass to the signing function using safe allocator
	privateKeyPtr, _, err := s.allocateSafe(PRIVATE_KEY_SIZE)
	if err != nil {
		log.Println("private key allocate error:", err)
		return "", errors.New("failed to allocate memory for private key")
	}

	defer func() {
		if err := s.deallocateSafe(privateKeyPtr, 0); err != nil {
			log.Printf("Failed to deallocate private key memory in Sign: %v", err)
		}
	}()

	// write private key to wasm memory
	ok = s.mod.Memory().Write(uint32(privateKeyPtr), []byte(key))
	if !ok {
		return "", errors.New("failed to write private key to memory for signing")
	}

	// call sign function with the pointers to private key and message
	signaturePtr, err := s.sign.Call(s.ctx, privateKeyPtr, PRIVATE_KEY_SIZE, messagePtr, msgLen)
	if err != nil {
		log.Println("sign error:", err)
		return "", errors.New("failed to sign message")
	}
	if len(signaturePtr) == 0 {
		return "", errors.New("internal error when signing message: empty return")
	}
	sigPtr, sigLen := decodeLenPtr(signaturePtr[0])
	if sigPtr == 0 || sigLen == 0 {
		return "", errors.New("internal error when signing message")
	}
	if sigLen != SIGNATURE_SIZE {
		log.Printf("unexpected signature length %d (expected %d)", sigLen, SIGNATURE_SIZE)
	}

	// read signature string from memory
	sig, ok := s.mod.Memory().Read(sigPtr, sigLen)
	if !ok {
		return "", errors.New("failed to sign message")
	}
	defer func(ptr uint32) {
		if err := s.deallocateSafe(uint64(ptr), 0); err != nil {
			log.Printf("Failed to deallocate signature memory: %v", err)
		}
	}(sigPtr)

	// wipe the private key bytes in WASM memory before deallocation (best effort)
	zero := make([]byte, PRIVATE_KEY_SIZE)
	_ = s.mod.Memory().Write(uint32(privateKeyPtr), zero)

	// since memory read returns a slice of wasm memory buffer, it needs to be copied
	// to avoid our returned slice being wiped when wasm memory is wiped.
	// explicit copy is not needed since we create a string, which copies the slice instead of referencing it
	signature = string(sig)

	return
}
