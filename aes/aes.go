package aes

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"

	"github.com/docker/docker/pkg/homedir"
)

var KEY_AES []byte

func InitAESKey(dir string) error {
	var filename string
	if dir != "" {
		filename = filepath.Join(dir, ".docker/aeskey")
	} else {
		filename = filepath.Join(homedir.Get(), ".docker/aeskey")
	}

	if _, err := os.Stat(filename); err == nil {
		if KEY_AES, err = ioutil.ReadFile(filename); err != nil {
			return err
		}
	} else if os.IsNotExist(err) {
		if err := os.MkdirAll(filepath.Dir(filename), 0700); err != nil {
			return err
		}

		// NewCipher creates and returns a new cipher.Block.
		// The key argument should be the AES key, either 16, 24, or 32 bytes to select AES-128, AES-192, or AES-256.
		if key, err := PRNG(32); err != nil {
			return err
		} else {
			KEY_AES = key
		}
		if err := ioutil.WriteFile(filename, KEY_AES, 0600); err != nil {
			return err
		}
	}
	return nil
}

// AESEncrypt will encrypt the message with cipher feedback mode with the given key.
func AESEncrypt(message, AESKey []byte) ([]byte, error) {
	block, err := aes.NewCipher(AESKey)
	if err != nil {
		return nil, err
	}

	// The IV needs to be unique, but not secure. Therefore it's common to
	// include it at the beginning of the ciphertext.
	ciphertext := make([]byte, aes.BlockSize+len(message))
	iv := ciphertext[:aes.BlockSize]
	if _, err := io.ReadFull(rand.Reader, iv); err != nil {
		return nil, err
	}
	stream := cipher.NewCFBEncrypter(block, iv)
	stream.XORKeyStream(ciphertext[aes.BlockSize:], message)

	return ciphertext, nil
}

// AESDecrypt will decrypt the ciphertext via the given key.
func AESDecrypt(ciphertext, AESKey []byte) ([]byte, error) {
	block, err := aes.NewCipher(AESKey)
	if err != nil {
		return nil, err
	}

	// The IV needs to be unique, but not secure. Therefore it's common to
	// include it at the beginning of the ciphertext.
	if len(ciphertext) < aes.BlockSize {
		return nil, fmt.Errorf("ciphertext too short")
	}
	iv := ciphertext[:aes.BlockSize]
	ciphertext = ciphertext[aes.BlockSize:]
	stream := cipher.NewCFBDecrypter(block, iv)

	// XORKeyStream can work in-place if the two arguments are the same.
	stream.XORKeyStream(ciphertext, ciphertext)
	return ciphertext, nil
}

// Pseudo Random Number Generator
func PRNG(size int) ([]byte, error) {
	b := make([]byte, size)
	_, err := rand.Read(b)
	if err != nil {
		return nil, err
	}
	return b, nil
}
