package esatompubpg

import (
	"crypto/aes"
	"crypto/cipher"
	"io"
	"github.com/aws/aws-sdk-go/service/kms"
	"os"
	"github.com/aws/aws-sdk-go/aws"
	"encoding/base64"
	"fmt"
	"crypto/rand"
	log "github.com/Sirupsen/logrus"
	"github.com/aws/aws-sdk-go/aws/session"
)
//KMS service
var kmsSvc *kms.KMS

func init() {
	keyAlias := KeyAliasRoot + os.Getenv(KeyAlias)

	if keyAlias != "" {
		log.Infof("Key alias specified: %s", keyAlias)
		log.Infof("AWS_REGION: %s", os.Getenv("AWS_REGION"))
		log.Infof("AWS_PROFILE: %s", os.Getenv("AWS_PROFILE"))

		sess, err := session.NewSession()
		if err == nil {
			kmsSvc = kms.New(sess)

			err = CheckKMSConfig()
			if err != nil {
				log.Errorf("Error instantiating AWS session: %s. Exiting.", err.Error())
				os.Exit(1)
			}
		} else {
			log.Infof("Error instantiating AWS session: %s. Exiting.", err.Error())
			os.Exit(1)
		}

	}
}


func CheckKMSConfig() error {
	keyAlias := KeyAliasRoot + os.Getenv(KeyAlias)
	if keyAlias == KeyAliasRoot {
		return nil
	}

	params := &kms.GenerateDataKeyInput{
		KeyId:   aws.String(keyAlias), // Required
		KeySpec: aws.String("AES_256"),
	}

	_, err := kmsSvc.GenerateDataKey(params)
	return err
}

//Encrypt from cryptopasta commit bc3a108a5776376aa811eea34b93383837994340
//used via the CC0 license. See https://github.com/gtank/cryptopasta
func Encrypt(plaintext []byte, key *[32]byte) ([]byte, error) {
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	nonce := make([]byte, gcm.NonceSize())
	_, err = io.ReadFull(rand.Reader, nonce)
	if err != nil {
		return nil, err
	}

	return gcm.Seal(nonce, nonce, plaintext, nil), nil
}

//Encrypt output encrypts the output is indicated by the configuration settings, e.g.
//KEY_ALIAS set to something. Here we obtain the encryption key from KMS, and append the
//encrypted version of the key to the encoded output.
func encryptOutput(svc *kms.KMS, out []byte) ([]byte, error) {
	keyAlias := KeyAliasRoot + os.Getenv(KeyAlias)
	if keyAlias == KeyAliasRoot {
		return out, nil
	}

	//Get the encryption keys
	params := &kms.GenerateDataKeyInput{
		KeyId:   aws.String(keyAlias), // Required
		KeySpec: aws.String("AES_256"),
	}

	resp, err := svc.GenerateDataKey(params)
	if err != nil {
		return nil, err
	}

	key := [32]byte{}
	copy(key[:], resp.Plaintext[0:32])

	//Encrypt the output
	encrypted, err := Encrypt(out, &key)
	if err != nil {
		return nil, err
	}

	//Purge the key from memory
	key = [32]byte{}
	resp.Plaintext = nil

	//Encode the output
	encodedOut := base64.StdEncoding.EncodeToString(encrypted)

	//Encode the encryptedKey - this will have to be decrypted using the KMS
	//CMK before the payload can be decrypted with it
	encodedKey := base64.StdEncoding.EncodeToString(resp.CiphertextBlob)

	keyPlusText := fmt.Sprintf("%s::%s", encodedKey, encodedOut)

	return []byte(keyPlusText), nil
}
