// Copyright (c) 2019 Perlin
//
// Permission is hereby granted, free of charge, to any person obtaining a copy of
// this software and associated documentation files (the "Software"), to deal in
// the Software without restriction, including without limitation the rights to
// use, copy, modify, merge, publish, distribute, sublicense, and/or sell copies of
// the Software, and to permit persons to whom the Software is furnished to do so,
// subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in all
// copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY, FITNESS
// FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE AUTHORS OR
// COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER LIABILITY, WHETHER
// IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM, OUT OF OR IN
// CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE SOFTWARE.

package wavelet

import (
	"encoding/hex"
	"errors"
	"fmt"
	"github.com/go-logr/logr"
	"github.com/perlin-network/noise/edwards25519"
	"github.com/perlin-network/noise/skademlia"
	"github.com/valyala/fastjson"
	"io/ioutil"
	"os"
	"path/filepath"
)

const (
	GenPath = "config"
	C1      = 16
	C2      = 16
)

func createGenesis(logger logr.Logger, n uint) (string, error) {
	if err := os.Mkdir(GenPath, 0644); err != nil && !os.IsExist(err) {
		if os.IsPermission(err) {
			return "", fmt.Errorf("failed to get permission to create directory %q to store wallets in", GenPath)
		}

		return "", fmt.Errorf("an unknown error occured creating directory %q", GenPath)
	}

	genesis := fastjson.MustParse(`{}`)
	balance := fastjson.MustParse(`{"balance": 10000000000000000000}`)

	for i := uint(1); i < n; i++ { // Exclude 1 wallet because we already include 1 additional wallet by default.
		walletFilePath := filepath.Join(GenPath, fmt.Sprintf("wallet%d.txt", i))

		if buf, err := ioutil.ReadFile(walletFilePath); err == nil && len(buf) == hex.EncodedLen(edwards25519.SizePrivateKey) {
			var privateKey edwards25519.PrivateKey

			if _, err := hex.Decode(privateKey[:], buf); err != nil {
				return "", err
			}

			genesis.Set(
				hex.EncodeToString(privateKey[edwards25519.SizePrivateKey/2:]),
				balance,
			)

			continue
		}

		keys, err := skademlia.NewKeys(C1, C2)

		if err != nil {
			return "", err
		}

		privateKey := keys.PrivateKey()

		privateKeyBuf := make([]byte, hex.EncodedLen(edwards25519.SizePrivateKey))

		if n := hex.Encode(privateKeyBuf[:], privateKey[:]); n != hex.EncodedLen(edwards25519.SizePrivateKey) {
			return "", errors.New("an unknown error occurred marshaling a newly generated keypairs private key into hex")
		}

		if err := ioutil.WriteFile(walletFilePath, privateKeyBuf, 0644); err != nil {
			return "", err
		}

		logger.Info("Generated a wallet.", "path", walletFilePath)

		genesis.Set(
			hex.EncodeToString(privateKey[edwards25519.SizePrivateKey/2:]),
			balance,
		)
	}

	return genesis.String(), nil
}
