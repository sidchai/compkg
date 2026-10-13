package yinbao

import (
	"strings"
	"testing"
)

func TestSM4RoundTripAndSignMaterialIsPlaintext(t *testing.T) {
	key := []byte("e8247f5ac43f8bd5")
	plain := `{"deviceNo":"SSYX41019095","onlineStatus":1}`
	cipherHex, err := encryptHex(key, plain)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(cipherHex, "{") {
		t.Fatal("ciphertext should be hex")
	}
	got, err := decryptHex(key, cipherHex)
	if err != nil {
		t.Fatal(err)
	}
	if got != plain {
		t.Fatalf("plain = %s", got)
	}
	salt := "908ac3b09aba97699ebd1dfbe4060088"
	sign := signSM3("1791873823519", "41502f795e31119f842ecde11cb1adc1", plain, salt)
	if len(sign) != 64 {
		t.Fatalf("sign len %d", len(sign))
	}
	if signSM3("1791873823519", "41502f795e31119f842ecde11cb1adc1", cipherHex, salt) == sign {
		t.Fatal("sign must use plaintext, not ciphertext")
	}
}

func TestDecryptCustomerSample(t *testing.T) {
	cipherHex := "9ed250bd2b330b26a2378a0627e3338736c249a46000a1da47425a9df9f87e00ea0a7588c4419ee2ad82c00b9e6e0693571c2936c7aafdbcdecf4ad6bdc8f20698b5b210cab38a7f63d75c84fd6b67cabd19646db8fdad20f3cebf9662d5befed65df05767acfa977b6385d1b5b6bbf7d5948b525e5fda6db2f704779b35f24a5d03b19103ddd187fe89776e60c87a1dc52039891c0c25f64dba498ad249f07d956790b3c61e0b8efae0c26d90c756f8c8f3e78fdc79705862dec936cc6e90170fa6a20a18305e44ef258ca50920fb36a262191120b04a74fbb9d41c97ddde750096ed9f094f5da8c4c7ade11f79851afdfbe6d6575546acdb6477c90eb9202645e2c8043c7ba9a2b331a081297294985004afd47999f3d2cbe1063cb75baae3638c14a511dd7a0196c91412bf9e09df63f5dd26606b80da5b6e7515b73ac0a66ed45d4670dee8facedff73c60d259d02257ffc1dbb88800785935713c7aa2c686c23a82a026a1199e883fcf94ed792a12a93b411b9ad1339f3690792dc7ce195b3f66d9c6f57ce4cc3aa1e1c764ec02a2fb455d2357c6af9fdf159b35cf7e8a22749c24b5dbb2648178c0d8db4fcc697fad8e1fa3b440641a5b84ee7015b3c90d912ca3138c9e172417bde6a7d3330d95a45afec473e8e67ffe17be01cd224b2f47469d86b1a1260f82e11377f59ed59ddfa926d94c3b6277945ac01cb579ad3f0b49919abb81721e442f66874c69ae91fd3bb03b8447a2a053e3276533de5d69e8b790553c9c29cddfc402fa0639b31ece5baeed29923885e67a10d4bbab2ce451db6465e778eebd1cbedce21c5bff4ecdbadb063c7cf26daefdf24dade3ab9a73893ea26ee476da226fad06511e5c"
	plain, err := decryptHex([]byte("e8247f5ac43f8bd5"), cipherHex)
	if err != nil {
		t.Fatal(err)
	}
	sign := signSM3("1791873823519", "41502f795e31119f842ecde11cb1adc1", plain, "908ac3b09aba97699ebd1dfbe4060088")
	const want = "01f8fe7653f37f7687050134b84dc3d8fb33dccb9481e3cefc894f4c217cc580"
	if sign != want {
		t.Fatalf("sign mismatch\nplain=%s\ngot=%s", plain, sign)
	}
	if !strings.HasPrefix(plain, "{") {
		t.Fatalf("plain is not json: %s", plain)
	}
}

func TestHostMatchOnlyConfiguredHost(t *testing.T) {
	hosts := []string{"biuptest.sinosig.com"}
	if !hostMatch("https://biuptest.sinosig.com:28080/csp-robot/deviceStatusCallback", hosts) {
		t.Fatal("expected match")
	}
	if hostMatch("https://customer.example.com/callback", hosts) {
		t.Fatal("other customer must not match")
	}
	if hostMatch("https://biuptest.sinosig.com.evil.com/callback", hosts) {
		t.Fatal("suffix lookalike must not match")
	}
	if hostMatch("", hosts) || hostMatch("not a url", nil) {
		t.Fatal("empty config or bad url must not match")
	}
}

func TestDisabledConfigKeepsPlainPath(t *testing.T) {
	if (Config{}).Applicable("https://biuptest.sinosig.com:28080/csp-robot/deviceStatusCallback") {
		t.Fatal("zero config must not encrypt")
	}
	cfg := Config{Enable: true, CallbackHosts: []string{"biuptest.sinosig.com"}}
	if cfg.Applicable("https://other.example.com/callback") {
		t.Fatal("other host must stay plain")
	}
}
