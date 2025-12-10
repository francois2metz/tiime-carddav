package main

import (
	"context"
	"encoding/base64"
	"fmt"
	"log"
	"net/http"
	"path"
	"strconv"
	"strings"
	"sync"

	"github.com/emersion/go-vcard"
	"github.com/emersion/go-webdav"
	"github.com/emersion/go-webdav/carddav"
	tiime "github.com/francois2metz/steampipe-plugin-tiime/tiime/client"
)

type SharedState struct {
	mu      sync.Mutex
	clients map[string]*tiime.Client
}

func clientToVCard(client tiime.Client2) vcard.Card {
	card := make(vcard.Card)
	card.SetValue(vcard.FieldAddress, client.Address)
	card.SetValue(vcard.FieldFormattedName, client.Name)
	if client.Phone != "" {
		card.SetValue(vcard.FieldTelephone, client.Phone)
	}
	if client.Email != "" {
		card.SetValue(vcard.FieldEmail, client.Email)
	}
	vcard.ToV4(card)
	return card
}

func parseAddressBookPath(p string) (int64, error) {
	_, companyIDAsString := path.Split(p[:len(p)-1])
	companyID, err := strconv.ParseInt(companyIDAsString, 10, 0)
	if err != nil {
		return 0, err
	}
	return companyID, nil
}

func formatAddressBookPath(companyID int64) string {
	return fmt.Sprint("/me/contacts/", companyID, "/")
}

func parseContactPath(p string) (int64, int64, error) {
	dir, idAsString := path.Split(p)
	id, err := strconv.ParseInt(idAsString, 10, 0)
	if err != nil {
		return 0, 0, err
	}
	companyID, err := parseAddressBookPath(dir)
	if err != nil {
		return 0, 0, err
	}
	return companyID, id, nil
}

func formatContactPath(companyID int64, id int64) string {
	return fmt.Sprint("/me/contacts/", companyID, "/", id)
}

type tiimeBackend struct {
	client *tiime.Client
}

func (*tiimeBackend) CurrentUserPrincipal(ctx context.Context) (string, error) {
	return "/me/", nil
}

func (*tiimeBackend) AddressBookHomeSetPath(ctx context.Context) (string, error) {
	return "/me/contacts/", nil
}

func (b *tiimeBackend) ListAddressBooks(ctx context.Context) ([]carddav.AddressBook, error) {
	companies, err := b.client.GetCompanies(ctx)
	if err != nil {
		return nil, err
	}
	addressBooks := []carddav.AddressBook{}
	for _, company := range companies {
		addressBooks = append(addressBooks, carddav.AddressBook{
			Path:            formatAddressBookPath(company.ID),
			Name:            fmt.Sprint("Tiime ", company.Name),
			Description:     fmt.Sprint("Contacts Tiime de ", company.Name),
			MaxResourceSize: 100 * 1024,
		})
	}
	return addressBooks, nil
}

func (b *tiimeBackend) GetAddressBook(ctx context.Context, path string) (*carddav.AddressBook, error) {
	abs, err := b.ListAddressBooks(ctx)
	if err != nil {
		panic(err)
	}
	for _, ab := range abs {
		if ab.Path == path {
			return &ab, nil
		}
	}
	return nil, webdav.NewHTTPError(404, fmt.Errorf("not found"))
}

func (b *tiimeBackend) CreateAddressBook(ctx context.Context, ab *carddav.AddressBook) error {
	return fmt.Errorf("not supported")
}

func (*tiimeBackend) DeleteAddressBook(ctx context.Context, path string) error {
	return fmt.Errorf("not supported")
}

func (b *tiimeBackend) GetAddressObject(ctx context.Context, path string, req *carddav.AddressDataRequest) (*carddav.AddressObject, error) {
	companyID, id, err := parseContactPath(path)
	if err != nil {
		return nil, err
	}
	client, err := b.client.GetClient(ctx, companyID, id)
	if err != nil {
		return nil, err
	}
	card := clientToVCard(client)
	return &carddav.AddressObject{
		Path: formatContactPath(companyID, client.ID),
		ETag: "1",
		Card: card,
	}, nil
}

func (b *tiimeBackend) ListAddressObjects(ctx context.Context, path string, req *carddav.AddressDataRequest) ([]carddav.AddressObject, error) {
	opts := tiime.PaginationOpts{Start: 0, End: 100}
	addressObjects := []carddav.AddressObject{}
	companyID, err := parseAddressBookPath(path)
	if err != nil {
		return nil, err
	}
	for {
		clients, pagination, err := b.client.GetClients(ctx, companyID, opts)
		if err != nil {
			return nil, err
		}
		for _, client := range clients {
			card := clientToVCard(client)
			addressObjects = append(addressObjects, carddav.AddressObject{
				Path: formatContactPath(companyID, client.ID),
				ETag: "1",
				Card: card,
			})
		}
		if pagination.Max != "*" {
			break
		}
		opts.Start += 100
		opts.End += 100
	}

	return addressObjects, nil
}

func (b *tiimeBackend) QueryAddressObjects(ctx context.Context, path string, query *carddav.AddressBookQuery) ([]carddav.AddressObject, error) {
	req := carddav.AddressDataRequest{AllProp: true}
	if query != nil {
		req = query.DataRequest
	}
	all, err := b.ListAddressObjects(ctx, path, &req)
	if err != nil {
		return nil, err
	}

	return carddav.Filter(query, all)
}

func (*tiimeBackend) PutAddressObject(ctx context.Context, path string, card vcard.Card, opts *carddav.PutAddressObjectOptions) (*carddav.AddressObject, error) {
	return nil, fmt.Errorf("not supported")
}

func (*tiimeBackend) DeleteAddressObject(ctx context.Context, path string) error {
	return fmt.Errorf("not supported")
}

type CreateTiimeClient func(string, string) (*tiime.Client, error)

func createTiimeClient(email string, password string) (*tiime.Client, error) {
	config := tiime.ClientConfig{
		Email:    email,
		Password: password,
	}
	client, err := tiime.New(context.TODO(), config)
	if err != nil {
		return nil, err
	}
	return client, nil
}

func getUserEmailAndPasswordFromAuth(authorization string) (string, string, error) {
	auth := strings.Split(authorization, " ")
	if len(auth) != 2 {
		return "", "", fmt.Errorf("bad auth header")
	}
	if auth[0] != "Basic" {
		return "", "", fmt.Errorf("bad authorization scheme")
	}
	data, err := base64.StdEncoding.DecodeString(auth[1])
	if err != nil {
		return "", "", fmt.Errorf("base64 decoding fail")
	}
	usernamepassword := strings.Split(string(data), ":")
	if len(usernamepassword) != 2 {
		return "", "", fmt.Errorf("bad auth")
	}
	return usernamepassword[0], usernamepassword[1], nil
}

func GetOrCreateClient(createClient CreateTiimeClient, authorization string, sharedState *SharedState) (*tiime.Client, error) {
	sharedState.mu.Lock()
	defer sharedState.mu.Unlock()
	client := sharedState.clients[authorization]
	if client == nil {
		email, password, err := getUserEmailAndPasswordFromAuth(authorization)
		if err != nil {
			return nil, err
		}
		client, err = createClient(email, password)
		if err != nil {
			return nil, err
		}
		sharedState.clients[authorization] = client
	} else if client.ShouldRefreshToken() {
		err := client.RefreshToken(context.TODO())
		if err != nil {
			return nil, err
		}
	}
	return client, nil
}

func httpHandler(resp http.ResponseWriter, req *http.Request, createClient CreateTiimeClient, sharedState *SharedState) {
	resp.Header().Set("WWW-Authenticate", `Basic realm="Tiime"`)
	authorization := req.Header.Get("authorization")
	if authorization == "" {
		http.Error(resp, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
		return
	}
	client, err := GetOrCreateClient(createClient, authorization, sharedState)
	if err != nil {
		http.Error(resp, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
		return
	}

	b := &tiimeBackend{
		client: client,
	}
	h := &carddav.Handler{Backend: b}
	h.ServeHTTP(resp, req)
}

func main() {
	shared := SharedState{
		clients: make(map[string]*tiime.Client),
	}

	s := &http.Server{
		Addr: "0.0.0.0:1234",
		Handler: http.HandlerFunc(func(resp http.ResponseWriter, req *http.Request) {
			log.Println("Request", req.Method, req.URL)
			httpHandler(resp, req, createTiimeClient, &shared)
		}),
	}

	log.Println("CardDAV server listening on", s.Addr)
	log.Fatal(s.ListenAndServe())
}
