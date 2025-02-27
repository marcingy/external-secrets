/*
Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package keyvault

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"testing"

	"github.com/Azure/azure-sdk-for-go/services/keyvault/2016-10-01/keyvault"
	tassert "github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientfake "sigs.k8s.io/controller-runtime/pkg/client/fake"

	esv1alpha1 "github.com/external-secrets/external-secrets/apis/externalsecrets/v1alpha1"
	v1 "github.com/external-secrets/external-secrets/apis/meta/v1"
	fake "github.com/external-secrets/external-secrets/pkg/provider/azure/keyvault/fake"
	"github.com/external-secrets/external-secrets/pkg/provider/schema"
	utils "github.com/external-secrets/external-secrets/pkg/utils"
)

type secretManagerTestCase struct {
	mockClient     *fake.AzureMockClient
	secretName     string
	secretVersion  string
	serviceURL     string
	ref            *esv1alpha1.ExternalSecretDataRemoteRef
	apiErr         error
	secretOutput   keyvault.SecretBundle
	keyOutput      keyvault.KeyBundle
	certOutput     keyvault.CertificateBundle
	expectError    string
	expectedSecret string
	// for testing secretmap
	expectedData map[string][]byte
}

func makeValidSecretManagerTestCase() *secretManagerTestCase {
	secretString := "Hello World!"
	smtc := secretManagerTestCase{
		mockClient:     &fake.AzureMockClient{},
		secretName:     "MySecret",
		secretVersion:  "",
		ref:            makeValidRef(),
		secretOutput:   keyvault.SecretBundle{Value: &secretString},
		serviceURL:     "",
		apiErr:         nil,
		expectError:    "",
		expectedSecret: secretString,
		expectedData:   map[string][]byte{},
	}

	smtc.mockClient.WithValue(smtc.serviceURL, smtc.secretName, smtc.secretVersion, smtc.secretOutput, smtc.apiErr)

	return &smtc
}

func makeValidSecretManagerTestCaseCustom(tweaks ...func(smtc *secretManagerTestCase)) *secretManagerTestCase {
	smtc := makeValidSecretManagerTestCase()
	for _, fn := range tweaks {
		fn(smtc)
	}

	smtc.mockClient.WithValue(smtc.serviceURL, smtc.secretName, smtc.secretVersion, smtc.secretOutput, smtc.apiErr)
	smtc.mockClient.WithKey(smtc.serviceURL, smtc.secretName, smtc.secretVersion, smtc.keyOutput, smtc.apiErr)
	smtc.mockClient.WithCertificate(smtc.serviceURL, smtc.secretName, smtc.secretVersion, smtc.certOutput, smtc.apiErr)

	return smtc
}

func TestNewClientManagedIdentityNoNeedForCredentials(t *testing.T) {
	namespace := "internal"
	vaultURL := "https://local.vault.url"
	identityID := "1234"
	authType := esv1alpha1.ManagedIdentity
	store := esv1alpha1.SecretStore{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
		},
		Spec: esv1alpha1.SecretStoreSpec{Provider: &esv1alpha1.SecretStoreProvider{AzureKV: &esv1alpha1.AzureKVProvider{
			AuthType:   &authType,
			IdentityID: &identityID,
			VaultURL:   &vaultURL,
		}}},
	}

	provider, err := schema.GetProvider(&store)
	tassert.Nil(t, err, "the return err should be nil")
	k8sClient := clientfake.NewClientBuilder().Build()
	secretClient, err := provider.NewClient(context.Background(), &store, k8sClient, namespace)
	if err != nil {
		// On non Azure environment, MSI auth not available, so this error should be returned
		tassert.EqualError(t, err, "failed to get oauth token from MSI: MSI not available")
	} else {
		// On Azure (where GitHub Actions are running) a secretClient is returned, as only an Authorizer is configured, but no token is requested for MI
		tassert.NotNil(t, secretClient)
	}
}

func TestNewClientNoCreds(t *testing.T) {
	namespace := "internal"
	vaultURL := "https://local.vault.url"
	tenantID := "1234"
	authType := esv1alpha1.ServicePrincipal
	store := esv1alpha1.SecretStore{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
		},
		Spec: esv1alpha1.SecretStoreSpec{Provider: &esv1alpha1.SecretStoreProvider{AzureKV: &esv1alpha1.AzureKVProvider{
			AuthType: &authType,
			VaultURL: &vaultURL,
			TenantID: &tenantID,
		}}},
	}
	provider, err := schema.GetProvider(&store)
	tassert.Nil(t, err, "the return err should be nil")
	k8sClient := clientfake.NewClientBuilder().Build()
	_, err = provider.NewClient(context.Background(), &store, k8sClient, namespace)
	tassert.EqualError(t, err, "missing clientID/clientSecret in store config")

	store.Spec.Provider.AzureKV.AuthSecretRef = &esv1alpha1.AzureKVAuth{}
	_, err = provider.NewClient(context.Background(), &store, k8sClient, namespace)
	tassert.EqualError(t, err, "missing accessKeyID/secretAccessKey in store config")

	store.Spec.Provider.AzureKV.AuthSecretRef.ClientID = &v1.SecretKeySelector{Name: "user"}
	_, err = provider.NewClient(context.Background(), &store, k8sClient, namespace)
	tassert.EqualError(t, err, "missing accessKeyID/secretAccessKey in store config")

	store.Spec.Provider.AzureKV.AuthSecretRef.ClientSecret = &v1.SecretKeySelector{Name: "password"}
	_, err = provider.NewClient(context.Background(), &store, k8sClient, namespace)
	tassert.EqualError(t, err, "could not find secret internal/user: secrets \"user\" not found")
	store.TypeMeta.Kind = esv1alpha1.ClusterSecretStoreKind
	store.TypeMeta.APIVersion = esv1alpha1.ClusterSecretStoreKindAPIVersion
	ns := "default"
	store.Spec.Provider.AzureKV.AuthSecretRef.ClientID.Namespace = &ns
	store.Spec.Provider.AzureKV.AuthSecretRef.ClientSecret.Namespace = &ns
	_, err = provider.NewClient(context.Background(), &store, k8sClient, namespace)
	tassert.EqualError(t, err, "could not find secret default/user: secrets \"user\" not found")
}

const (
	jwkPubRSA            = `{"kid":"ex","kty":"RSA","key_ops":["sign","verify","wrapKey","unwrapKey","encrypt","decrypt"],"n":"p2VQo8qCfWAZmdWBVaYuYb-a-tWWm78K6Sr9poCvNcmv8rUPSLACxitQWR8gZaSH1DklVkqz-Ed8Cdlf8lkDg4Ex5tkB64jRdC1Uvn4CDpOH6cp-N2s8hTFLqy9_YaDmyQS7HiqthOi9oVjil1VMeWfaAbClGtFt6UnKD0Vb_DvLoWYQSqlhgBArFJi966b4E1pOq5Ad02K8pHBDThlIIx7unibLehhDU6q3DCwNH_OOLx6bgNtmvGYJDd1cywpkLQ3YzNCUPWnfMBJRP3iQP_WI21uP6cvo0DqBPBM4wvVzHbCT0vnIflwkbgEWkq1FprqAitZlop9KjLqzjp9vyQ","e":"AQAB"}`
	jwkPubEC             = `{"kid":"https://example.vault.azure.net/keys/ec-p-521/e3d0e9c179b54988860c69c6ae172c65","kty":"EC","key_ops":["sign","verify"],"crv":"P-521","x":"AedOAtb7H7Oz1C_cPKI_R4CN_eai5nteY6KFW07FOoaqgQfVCSkQDK22fCOiMT_28c8LZYJRsiIFz_IIbQUW7bXj","y":"AOnchHnmBphIWXvanmMAmcCDkaED6ycW8GsAl9fQ43BMVZTqcTkJYn6vGnhn7MObizmkNSmgZYTwG-vZkIg03HHs"}`
	jsonTestString       = `{"Name": "External", "LastName": "Secret", "Address": { "Street": "Myroad st.", "CP": "J4K4T4" } }`
	jsonSingleTestString = `{"Name": "External", "LastName": "Secret" }`
	keyName              = "key/keyname"
	certName             = "cert/certname"
)

func newKVJWK(b []byte) *keyvault.JSONWebKey {
	var key keyvault.JSONWebKey
	err := json.Unmarshal(b, &key)
	if err != nil {
		panic(err)
	}
	return &key
}

// test the sm<->azurekv interface
// make sure correct values are passed and errors are handled accordingly.
func TestAzureKeyVaultSecretManagerGetSecret(t *testing.T) {
	secretString := "changedvalue"
	secretCertificate := "certificate_value"

	// good case
	setSecretString := func(smtc *secretManagerTestCase) {
		smtc.expectedSecret = secretString
		smtc.secretOutput = keyvault.SecretBundle{
			Value: &secretString,
		}
	}

	setSecretStringWithVersion := func(smtc *secretManagerTestCase) {
		smtc.expectedSecret = secretString
		smtc.secretOutput = keyvault.SecretBundle{
			Value: &secretString,
		}
		smtc.ref.Version = "v1"
		smtc.secretVersion = smtc.ref.Version
	}

	setSecretWithProperty := func(smtc *secretManagerTestCase) {
		jsonString := jsonTestString
		smtc.expectedSecret = "External"
		smtc.secretOutput = keyvault.SecretBundle{
			Value: &jsonString,
		}
		smtc.ref.Property = "Name"
	}

	badSecretWithProperty := func(smtc *secretManagerTestCase) {
		jsonString := jsonTestString
		smtc.expectedSecret = ""
		smtc.secretOutput = keyvault.SecretBundle{
			Value: &jsonString,
		}
		smtc.ref.Property = "Age"
		smtc.expectError = fmt.Sprintf("property %s does not exist in key %s", smtc.ref.Property, smtc.ref.Key)
		smtc.apiErr = fmt.Errorf(smtc.expectError)
	}

	// // good case: key set
	setPubRSAKey := func(smtc *secretManagerTestCase) {
		smtc.secretName = keyName
		smtc.expectedSecret = jwkPubRSA
		smtc.keyOutput = keyvault.KeyBundle{
			Key: newKVJWK([]byte(jwkPubRSA)),
		}
		smtc.ref.Key = smtc.secretName
	}

	// // good case: key set
	setPubECKey := func(smtc *secretManagerTestCase) {
		smtc.secretName = keyName
		smtc.expectedSecret = jwkPubEC
		smtc.keyOutput = keyvault.KeyBundle{
			Key: newKVJWK([]byte(jwkPubEC)),
		}
		smtc.ref.Key = smtc.secretName
	}

	// // good case: key set
	setCertificate := func(smtc *secretManagerTestCase) {
		byteArrString := []byte(secretCertificate)
		smtc.secretName = certName
		smtc.expectedSecret = secretCertificate
		smtc.certOutput = keyvault.CertificateBundle{
			Cer: &byteArrString,
		}
		smtc.ref.Key = smtc.secretName
	}

	badSecretType := func(smtc *secretManagerTestCase) {
		smtc.secretName = "name"
		smtc.expectedSecret = ""
		smtc.expectError = fmt.Sprintf("unknown Azure Keyvault object Type for %s", smtc.secretName)
		smtc.ref.Key = fmt.Sprintf("dummy/%s", smtc.secretName)
	}

	successCases := []*secretManagerTestCase{
		makeValidSecretManagerTestCase(),
		makeValidSecretManagerTestCaseCustom(setSecretString),
		makeValidSecretManagerTestCaseCustom(setSecretStringWithVersion),
		makeValidSecretManagerTestCaseCustom(setSecretWithProperty),
		makeValidSecretManagerTestCaseCustom(badSecretWithProperty),
		makeValidSecretManagerTestCaseCustom(setPubRSAKey),
		makeValidSecretManagerTestCaseCustom(setPubECKey),
		makeValidSecretManagerTestCaseCustom(setCertificate),
		makeValidSecretManagerTestCaseCustom(badSecretType),
	}

	sm := Azure{}
	for k, v := range successCases {
		sm.baseClient = v.mockClient
		out, err := sm.GetSecret(context.Background(), *v.ref)
		if !utils.ErrorContains(err, v.expectError) {
			t.Errorf("[%d] unexpected error: %s, expected: '%s'", k, err.Error(), v.expectError)
		}
		if string(out) != v.expectedSecret {
			t.Errorf("[%d] unexpected secret: expected %s, got %s", k, v.expectedSecret, string(out))
		}
	}
}

func TestAzureKeyVaultSecretManagerGetSecretMap(t *testing.T) {
	secretString := "changedvalue"
	secretCertificate := "certificate_value"

	badSecretString := func(smtc *secretManagerTestCase) {
		smtc.expectedSecret = secretString
		smtc.secretOutput = keyvault.SecretBundle{
			Value: &secretString,
		}
		smtc.expectError = "error unmarshalling json data: invalid character 'c' looking for beginning of value"
	}

	setSecretJSON := func(smtc *secretManagerTestCase) {
		jsonString := jsonSingleTestString
		smtc.secretOutput = keyvault.SecretBundle{
			Value: &jsonString,
		}
		smtc.expectedData["Name"] = []byte("External")
		smtc.expectedData["LastName"] = []byte("Secret")
	}

	setSecretJSONWithProperty := func(smtc *secretManagerTestCase) {
		jsonString := jsonTestString
		smtc.secretOutput = keyvault.SecretBundle{
			Value: &jsonString,
		}
		smtc.ref.Property = "Address"

		smtc.expectedData["Street"] = []byte("Myroad st.")
		smtc.expectedData["CP"] = []byte("J4K4T4")
	}

	badSecretWithProperty := func(smtc *secretManagerTestCase) {
		jsonString := jsonTestString
		smtc.expectedSecret = ""
		smtc.secretOutput = keyvault.SecretBundle{
			Value: &jsonString,
		}
		smtc.ref.Property = "Age"
		smtc.expectError = fmt.Sprintf("property %s does not exist in key %s", smtc.ref.Property, smtc.ref.Key)
		smtc.apiErr = fmt.Errorf(smtc.expectError)
	}

	badPubRSAKey := func(smtc *secretManagerTestCase) {
		smtc.secretName = keyName
		smtc.expectedSecret = jwkPubRSA
		smtc.keyOutput = keyvault.KeyBundle{
			Key: newKVJWK([]byte(jwkPubRSA)),
		}
		smtc.ref.Key = smtc.secretName
		smtc.expectError = "cannot get use dataFrom to get key secret"
	}

	badCertificate := func(smtc *secretManagerTestCase) {
		byteArrString := []byte(secretCertificate)
		smtc.secretName = certName
		smtc.expectedSecret = secretCertificate
		smtc.certOutput = keyvault.CertificateBundle{
			Cer: &byteArrString,
		}
		smtc.ref.Key = smtc.secretName
		smtc.expectError = "cannot get use dataFrom to get certificate secret"
	}

	badSecretType := func(smtc *secretManagerTestCase) {
		smtc.secretName = "name"
		smtc.expectedSecret = ""
		smtc.expectError = fmt.Sprintf("unknown Azure Keyvault object Type for %s", smtc.secretName)
		smtc.ref.Key = fmt.Sprintf("dummy/%s", smtc.secretName)
	}

	successCases := []*secretManagerTestCase{
		makeValidSecretManagerTestCaseCustom(badSecretString),
		makeValidSecretManagerTestCaseCustom(setSecretJSON),
		makeValidSecretManagerTestCaseCustom(setSecretJSONWithProperty),
		makeValidSecretManagerTestCaseCustom(badSecretWithProperty),
		makeValidSecretManagerTestCaseCustom(badPubRSAKey),
		makeValidSecretManagerTestCaseCustom(badCertificate),
		makeValidSecretManagerTestCaseCustom(badSecretType),
	}

	sm := Azure{}
	for k, v := range successCases {
		sm.baseClient = v.mockClient
		out, err := sm.GetSecretMap(context.Background(), *v.ref)
		if !utils.ErrorContains(err, v.expectError) {
			t.Errorf("[%d] unexpected error: %s, expected: '%s'", k, err.Error(), v.expectError)
		}
		if err == nil && !reflect.DeepEqual(out, v.expectedData) {
			t.Errorf("[%d] unexpected secret data: expected %#v, got %#v", k, v.expectedData, out)
		}
	}
}

func makeValidRef() *esv1alpha1.ExternalSecretDataRemoteRef {
	return &esv1alpha1.ExternalSecretDataRemoteRef{
		Key:     "test-secret",
		Version: "default",
	}
}
