package jwt

import "testing"

func TestSingleKeyVerifier(t *testing.T) {
	keyInvalid := "{}"
	keyValid := "{\"use\":\"sign\",\"kty\":\"oct\",\"kid\":\"005456ff-1262-4bf0-a608-8534e1fe2763\",\"alg\":\"HS256\",\"k\":\"L0FCL4hivd7ShePdJnzEEoqlwoOfCrkcqdbXdADNk0s523xV7C5Sr6GiRIMpvNIelEsR6ta7MZnELY4JoHrm_w\"}"
	tokenInvalid := "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.e30.piBniOUxc9Mf51x9KrOhN1ZYfkmiNCsHBIRLDShjD2M"
	tokenValid := "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.e30.TucQsITYiBvDjkOC4zk4Uj-hug6_usC_OjAuheinuUw"
	if _, err := NewSingleKeyVerifier(keyInvalid); err == nil {
		t.Errorf("invalid key not detected")
	}
	svk, err := NewSingleKeyVerifier(keyValid)
	if err != nil {
		t.Fatalf("can't parse valid key: %v", err)
		return
	}
	if err := svk.Verify(tokenInvalid); err == nil || err.Error() != "JWT is not valid: signature is invalid" {
		t.Errorf("bad error for invalid token: %v", err)
	}
	if err := svk.Verify(tokenValid); err != nil {
		t.Errorf("bad error for valid token: %v", err)
	}
}
