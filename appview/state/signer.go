package state

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

type SignerTransport struct {
	Secret string
}

func (s SignerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	timestamp := time.Now().Format(time.RFC3339)
	mac := hmac.New(sha256.New, []byte(s.Secret))
	message := req.Method + req.URL.Path + timestamp
	mac.Write([]byte(message))
	signature := hex.EncodeToString(mac.Sum(nil))
	req.Header.Set("X-Signature", signature)
	req.Header.Set("X-Timestamp", timestamp)
	return http.DefaultTransport.RoundTrip(req)
}

type SignedClient struct {
	Secret string
	Url    *url.URL
	client *http.Client
}

func NewSignedClient(domain, secret string, dev bool) (*SignedClient, error) {
	client := &http.Client{
		Timeout: 5 * time.Second,
		Transport: SignerTransport{
			Secret: secret,
		},
	}

	scheme := "https"
	if dev {
		scheme = "http"
	}
	url, err := url.Parse(fmt.Sprintf("%s://%s", scheme, domain))
	if err != nil {
		return nil, err
	}

	signedClient := &SignedClient{
		Secret: secret,
		client: client,
		Url:    url,
	}

	return signedClient, nil
}

func (s *SignedClient) newRequest(method, endpoint string, body []byte) (*http.Request, error) {
	return http.NewRequest(method, s.Url.JoinPath(endpoint).String(), bytes.NewReader(body))
}

func (s *SignedClient) Init(did string) (*http.Response, error) {
	const (
		Method   = "POST"
		Endpoint = "/init"
	)

	body, _ := json.Marshal(map[string]any{
		"did": did,
	})

	req, err := s.newRequest(Method, Endpoint, body)
	if err != nil {
		return nil, err
	}

	return s.client.Do(req)
}

func (s *SignedClient) NewRepo(did, repoName, defaultBranch string) (*http.Response, error) {
	const (
		Method   = "PUT"
		Endpoint = "/repo/new"
	)

	body, _ := json.Marshal(map[string]any{
		"did":            did,
		"name":           repoName,
		"default_branch": defaultBranch,
	})

	req, err := s.newRequest(Method, Endpoint, body)
	if err != nil {
		return nil, err
	}

	return s.client.Do(req)
}

func (s *SignedClient) RemoveRepo(did, repoName string) (*http.Response, error) {
	const (
		Method   = "DELETE"
		Endpoint = "/repo"
	)

	body, _ := json.Marshal(map[string]any{
		"did":  did,
		"name": repoName,
	})

	req, err := s.newRequest(Method, Endpoint, body)
	if err != nil {
		return nil, err
	}

	return s.client.Do(req)
}

func (s *SignedClient) AddMember(did string) (*http.Response, error) {
	const (
		Method   = "PUT"
		Endpoint = "/member/add"
	)

	body, _ := json.Marshal(map[string]any{
		"did": did,
	})

	req, err := s.newRequest(Method, Endpoint, body)
	if err != nil {
		return nil, err
	}

	return s.client.Do(req)
}

func (s *SignedClient) AddCollaborator(ownerDid, repoName, memberDid string) (*http.Response, error) {
	const (
		Method = "POST"
	)
	endpoint := fmt.Sprintf("/%s/%s/collaborator/add", ownerDid, repoName)

	body, _ := json.Marshal(map[string]any{
		"did": memberDid,
	})

	req, err := s.newRequest(Method, endpoint, body)
	if err != nil {
		return nil, err
	}

	return s.client.Do(req)
}

func (s *SignedClient) Merge(patch []byte, ownerDid, targetRepo, branch string) (*http.Response, error) {
	const (
		Method = "POST"
	)
	endpoint := fmt.Sprintf("/%s/%s/merge", ownerDid, targetRepo)

	body, _ := json.Marshal(map[string]interface{}{
		"patch":  string(patch),
		"branch": branch,
	})

	req, err := s.newRequest(Method, endpoint, body)
	if err != nil {
		return nil, err
	}

	return s.client.Do(req)
}

func (s *SignedClient) MergeCheck(patch []byte, ownerDid, targetRepo, branch string) (*http.Response, error) {
	const (
		Method = "POST"
	)
	endpoint := fmt.Sprintf("/%s/%s/merge/check", ownerDid, targetRepo)

	body, _ := json.Marshal(map[string]interface{}{
		"patch":  string(patch),
		"branch": branch,
	})

	req, err := s.newRequest(Method, endpoint, body)
	if err != nil {
		return nil, err
	}

	return s.client.Do(req)
}

type UnsignedClient struct {
	Url    *url.URL
	client *http.Client
}

func NewUnsignedClient(domain string, dev bool) (*UnsignedClient, error) {
	client := &http.Client{
		Timeout: 5 * time.Second,
	}

	scheme := "https"
	if dev {
		scheme = "http"
	}
	url, err := url.Parse(fmt.Sprintf("%s://%s", scheme, domain))
	if err != nil {
		return nil, err
	}

	unsignedClient := &UnsignedClient{
		client: client,
		Url:    url,
	}

	return unsignedClient, nil
}

func (us *UnsignedClient) newRequest(method, endpoint string, body []byte) (*http.Request, error) {
	return http.NewRequest(method, us.Url.JoinPath(endpoint).String(), bytes.NewReader(body))
}

func (us *UnsignedClient) Index(ownerDid, repoName, ref string) (*http.Response, error) {
	const (
		Method = "GET"
	)

	endpoint := fmt.Sprintf("/%s/%s/tree/%s", ownerDid, repoName, ref)
	if ref == "" {
		endpoint = fmt.Sprintf("/%s/%s", ownerDid, repoName)
	}

	req, err := us.newRequest(Method, endpoint, nil)
	if err != nil {
		return nil, err
	}

	return us.client.Do(req)
}

func (us *UnsignedClient) Branches(ownerDid, repoName string) (*http.Response, error) {
	const (
		Method = "GET"
	)

	endpoint := fmt.Sprintf("/%s/%s/branches", ownerDid, repoName)

	req, err := us.newRequest(Method, endpoint, nil)
	if err != nil {
		return nil, err
	}

	return us.client.Do(req)
}
