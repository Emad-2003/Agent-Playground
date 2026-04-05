package oauth

import (
	"io"
	"os"

	"crawler-ai/internal/apikeys"
)

type KeyStore = apikeys.KeyStore

const DataDirEnvVar = apikeys.DataDirEnvVar

func DefaultKeyStore() *KeyStore {
	return apikeys.DefaultKeyStore()
}

func SetDefaultConfigDir(dir string) {
	apikeys.SetDefaultConfigDir(dir)
}

func ConfigDirOverride() string {
	return apikeys.ConfigDirOverride()
}

func ResolveProviderKey(store *KeyStore, provider, configKey string) string {
	return apikeys.Resolve(store, provider, configKey)
}

func PromptForProvider(in io.Reader, out io.Writer) (string, error) {
	return apikeys.PromptForProvider(in, out)
}

func PromptForKey(provider string) (string, error) {
	return apikeys.PromptForKey(provider)
}

func ConfirmOverwrite(provider string, in io.Reader, out io.Writer) (bool, error) {
	return apikeys.ConfirmOverwrite(provider, in, out)
}

func EnvVarForProvider(provider string) string {
	return apikeys.EnvVarForProvider(provider)
}

func DefaultConfigDir() string {
	return apikeys.DefaultConfigDir()
}

func Stdin() *os.File  { return os.Stdin }
func Stdout() *os.File { return os.Stdout }
