package esatompubpg

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	log "github.com/Sirupsen/logrus"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/kms"
	"github.com/xtracdev/envinject"
	"io"
)

const (
	KeyAliasRoot = "alias/"
	KeyAlias     = "KEY_ALIAS"
)

type AtomEncrypter struct {
	keyAlias string
	kmsSvc   *kms.KMS
}

func NewAtomEncrypter(env *envinject.InjectedEnv) (*AtomEncrypter, error) {
	if env == nil {
		return nil, ErrMissingInjectedEnv
	}

	encrypter := AtomEncrypter{}
	var kmsSvc *kms.KMS

	keyAlias := KeyAliasRoot + env.Getenv(KeyAlias)
	if keyAlias != KeyAliasRoot {
		encrypter.keyAlias = keyAlias

		log.Infof("Key alias specified: %s", keyAlias)
		log.Infof("AWS_REGION: %s", env.Getenv("AWS_REGION"))
		log.Infof("AWS_PROFILE: %s", env.Getenv("AWS_PROFILE"))

		sess, err := session.NewSession()
		if err != nil {
			return nil, err
		}

		kmsSvc = kms.New(sess)
		encrypter.kmsSvc = kmsSvc

		err = encrypter.CheckKMSConfig()
		if err != nil {
			return nil, err
		}
	}

	return &encrypter, nil
}

func (ae *AtomEncrypter) CheckKMSConfig() error {
	if ae.keyAlias == KeyAliasRoot {
		return nil
	}

	params := &kms.GenerateDataKeyInput{
		KeyId:   aws.String(ae.keyAlias), // Required
		KeySpec: aws.String("AES_256"),
	}

	_, err := ae.kmsSvc.GenerateDataKey(params)
	return err
}

//Encrypt from cryptopasta commit bc3a108a5776376aa811eea34b93383837994340
//used via the CC0 license. See https://github.com/gtank/cryptopasta
func encrypt(plaintext []byte, key *[32]byte) ([]byte, error) {
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

//EncryptOutput encrypts the output as indicated by the configuration settings, e.g.
//KEY_ALIAS set to something. Here we obtain the encryption key from KMS, and append the
//encrypted version of the key to the encoded output.
func (ae *AtomEncrypter) EncryptOutput(out []byte) ([]byte, error) {
	if ae.keyAlias == "" {
		return out, nil
	}

	//Get the encryption keys
	params := &kms.GenerateDataKeyInput{
		KeyId:   aws.String(ae.keyAlias), // Required
		KeySpec: aws.String("AES_256"),
	}

	resp, err := ae.kmsSvc.GenerateDataKey(params)
	if err != nil {
		return nil, err
	}

	key := [32]byte{}
	copy(key[:], resp.Plaintext[0:32])

	//Encrypt the output
	encrypted, err := encrypt(out, &key)
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
