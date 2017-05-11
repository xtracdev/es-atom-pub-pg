package main

import (
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"errors"
	"fmt"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/kms"
	"io/ioutil"
	"net/http"
	"os"
	"strings"
	"net/url"
	"crypto/tls"
)

//Decrypt from cryptopasta commit bc3a108a5776376aa811eea34b93383837994340
//used via the CC0 license. See https://github.com/gtank/cryptopasta
func Decrypt(ciphertext []byte, key *[32]byte) (plaintext []byte, err error) {
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	if len(ciphertext) < gcm.NonceSize() {
		return nil, errors.New("malformed ciphertext")
	}

	return gcm.Open(nil,
		ciphertext[:gcm.NonceSize()],
		ciphertext[gcm.NonceSize():],
		nil,
	)
}

func readRecent(feedUrl string) ([]byte, error) {

	parsed,err := url.Parse(feedUrl)
	if err != nil {
		return nil, err
	}

	client := http.DefaultClient
	if parsed.Scheme == "https" {
		tr := http.DefaultTransport
		defTransAsTransPort, ok := tr.(*http.Transport)
		if !ok {
			return nil, errors.New("Unable to coerce default transport to transport type")
		}
		defTransAsTransPort.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
		client = &http.Client{Transport: tr}
	}

	resp, err := client.Get(feedUrl)
	if err != nil {
		return nil, err
	}

	defer resp.Body.Close()

	bytes, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		fmt.Println(resp)
		return nil, errors.New("Status was not ok")
	}

	return bytes, nil
}

func decryptMessage(svc *kms.KMS, parts []string) ([]byte, error) {
	//Decode the key and the text
	keyBytes, err := base64.StdEncoding.DecodeString(parts[0])
	if err != nil {
		return nil, err
	}

	//Get the encrypted bytes
	msgBytes, err := base64.StdEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, err
	}

	//Decrypt the encryption key
	di := &kms.DecryptInput{
		CiphertextBlob: keyBytes,
	}

	decryptedKey, err := svc.Decrypt(di)
	if err != nil {
		return nil, err
	}

	//Use the decrypted key to decrypt the message text
	decryptKey := [32]byte{}

	copy(decryptKey[:], decryptedKey.Plaintext[0:32])

	return Decrypt(msgBytes, &decryptKey)

}

func main() {
	//Read the recent notifications page  and decrypt the content for grins
	if len(os.Args) != 2 {
		fmt.Printf("Usage: %s url\n", os.Args[0])
		return
	}

	//KMS set up
	sess, err := session.NewSession()
	if err != nil {
		fmt.Println(err.Error())
		return
	}

	svc := kms.New(sess)

	feedUrl := os.Args[1] + "/notifications/recent"

	for i := 0; i < 1; i++ {
		fmt.Println("Iteration ", i)
		bytes, err := readRecent(feedUrl)
		if err != nil {
			fmt.Println(err)
			break
		}

		//Now split the output into two parts - the encrypted key
		//and the encrypted text
		parts := strings.Split(string(bytes), "::")
		if len(parts) != 2 {
			fmt.Println("Expected two parts, got ", len(parts))
			break
		}

		decypted, err := decryptMessage(svc, parts)
		if err != nil {
			fmt.Println(err)
			break
		}

		fmt.Println("Decrypted :\n", string(decypted))
	}
}
