//go:build rehearsal

// Throwaway OIDC IdP for a local deploy rehearsal. Not part of the repository.
package main

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"math/big"
	"net/http"
	"net/url"
	"os"
	"sync"
	"time"
)

var b64 = base64.RawURLEncoding

type grant struct{ nonce, challenge, email string }

func main() {
	issuer := os.Getenv("ISSUER")
	clientID := os.Getenv("CLIENT_ID")
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		log.Fatal(err)
	}
	var mu sync.Mutex
	codes := map[string]grant{}
	writeJSON := func(w http.ResponseWriter, v any) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(v)
	}
	http.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"issuer": issuer, "authorization_endpoint": issuer + "/authorize", "token_endpoint": issuer + "/token",
			"userinfo_endpoint": issuer + "/userinfo", "jwks_uri": issuer + "/jwks", "end_session_endpoint": issuer + "/logout"})
	})
	http.HandleFunc("/jwks", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"keys": []map[string]string{{"kty": "RSA", "use": "sig", "alg": "RS256", "kid": "k1",
			"n": b64.EncodeToString(key.N.Bytes()), "e": b64.EncodeToString(big.NewInt(int64(key.E)).Bytes())}}})
	})
	http.HandleFunc("/authorize", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		raw := make([]byte, 16)
		rand.Read(raw)
		code := b64.EncodeToString(raw)
		mu.Lock()
		codes[code] = grant{q.Get("nonce"), q.Get("code_challenge"), q.Get("login_hint")}
		mu.Unlock()
		http.Redirect(w, r, q.Get("redirect_uri")+"?"+url.Values{"code": {code}, "state": {q.Get("state")}}.Encode(), http.StatusFound)
	})
	http.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		mu.Lock()
		g, ok := codes[r.Form.Get("code")]
		delete(codes, r.Form.Get("code"))
		mu.Unlock()
		sum := sha256.Sum256([]byte(r.Form.Get("code_verifier")))
		if !ok || (g.challenge != "" && b64.EncodeToString(sum[:]) != g.challenge) {
			http.Error(w, `{"error":"invalid_grant"}`, http.StatusBadRequest)
			return
		}
		hdr, _ := json.Marshal(map[string]string{"alg": "RS256", "kid": "k1", "typ": "JWT"})
		now := time.Now().Unix()
		claims, _ := json.Marshal(map[string]any{"iss": issuer, "sub": "sub-" + g.email, "aud": clientID, "iat": now, "exp": now + 3600,
			"nonce": g.nonce, "email": g.email, "name": g.email, "email_verified": true})
		input := b64.EncodeToString(hdr) + "." + b64.EncodeToString(claims)
		h := sha256.Sum256([]byte(input))
		sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, h[:])
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		writeJSON(w, map[string]any{"access_token": "at-" + g.email, "token_type": "Bearer", "expires_in": 3600, "id_token": input + "." + b64.EncodeToString(sig)})
	})
	http.HandleFunc("/userinfo", func(w http.ResponseWriter, r *http.Request) {
		email := ""
		fmt.Sscanf(r.Header.Get("Authorization"), "Bearer at-%s", &email)
		writeJSON(w, map[string]any{"sub": "sub-" + email, "email": email, "name": email, "email_verified": true})
	})
	log.Fatal(http.ListenAndServe(":8000", nil))
}
