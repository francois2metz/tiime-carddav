package main

import (
	"context"
	"encoding/base64"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/emersion/go-vcard"
	"github.com/emersion/go-webdav"
	"github.com/emersion/go-webdav/carddav"
	tiime "github.com/francois2metz/steampipe-plugin-tiime/tiime/client"
	"github.com/mitchellh/hashstructure/v2"
)

type SharedState struct {
	mu      sync.Mutex
	clients map[string]*tiime.Client
}

func clientProToVCard(client tiime.Client2, contacts []tiime.Contact) vcard.Card {
	card := make(vcard.Card)
	card.SetKind(vcard.KindGroup)
	card.SetValue(vcard.FieldUID, fmt.Sprint("client-", client.ID))
	card.SetValue(vcard.FieldFormattedName, client.Name)
	card.SetValue(vcard.FieldAddress, fmt.Sprint(client.Address, " ", client.City))
	for _, contact := range contacts {
		card.AddValue(vcard.FieldMember, fmt.Sprint(contact.ID))
	}
	if client.Phone != "" {
		card.SetValue(vcard.FieldTelephone, client.Phone)
	}
	if client.Email != "" {
		card.SetValue(vcard.FieldEmail, client.Email)
	}
	vcard.ToV4(card)
	return card
}

func contactClientToVCard(client tiime.Client2, contact tiime.Contact) vcard.Card {
	card := make(vcard.Card)
	card.SetValue(vcard.FieldUID, fmt.Sprint(contact.ID))
	card.SetValue(vcard.FieldAddress, fmt.Sprint(client.Address, " ", client.City))
	card.SetValue(vcard.FieldFormattedName, fmt.Sprint(contact.Firstname, " ", contact.Lastname))
	if client.Phone != "" {
		card.SetValue(vcard.FieldTelephone, client.Phone)
	}
	if contact.Phone != "" {
		card.SetValue(vcard.FieldTelephone, contact.Phone)
	}
	if client.Email != "" {
		card.SetValue(vcard.FieldEmail, client.Email)
	}
	if contact.Email != "" {
		card.SetValue(vcard.FieldEmail, contact.Email)
	}
	vcard.ToV4(card)
	return card
}

func toAddressObject(card vcard.Card, path string) *carddav.AddressObject {
	hash, err := hashstructure.Hash(card, hashstructure.FormatV2, nil)
	if err != nil {
		panic(err)
	}
	return &carddav.AddressObject{
		Path: path,
		ETag: fmt.Sprint(hash),
		Card: card,
	}
}

func formatAddressBookPath(companyID int64) string {
	return fmt.Sprint("/me/contacts/", companyID, "/")
}

func formatClientPath(companyID int64, id int64) string {
	return fmt.Sprint(formatAddressBookPath(companyID), id)
}

func formatContactPath(companyID int64, clientID int64, id int64) string {
	return fmt.Sprint(formatClientPath(companyID, clientID), "/", id)
}

func parseAddressBookPath(p string) (int64, error) {
	pattern := regexp.MustCompile(`^/me/contacts/([0-9]+).*$`)
	result := pattern.FindAllStringSubmatch(p, -1)
	if len(result) == 0 {
		return 0, fmt.Errorf("not an address book path")
	}
	companyID, err := strconv.ParseInt(result[0][1], 10, 0)
	if err != nil {
		return 0, err
	}
	return companyID, nil
}

func parseAddressPath(p string) (int64, int64, int64, error) {
	pattern := regexp.MustCompile(`^/me/contacts/([0-9]+)/([0-9]+)/?([0-9]+)?$`)
	result := pattern.FindAllStringSubmatch(p, -1)
	if len(result) == 0 {
		return 0, 0, 0, fmt.Errorf("not a client path")
	}
	clientID, err := strconv.ParseInt(result[0][2], 10, 0)
	if err != nil {
		return 0, 0, 0, err
	}
	var id int64
	if result[0][3] != "" {
		id, err = strconv.ParseInt(result[0][3], 10, 0)
		if err != nil {
			return 0, 0, 0, err
		}
	}
	companyID, err := parseAddressBookPath(p)
	if err != nil {
		return 0, 0, 0, err
	}
	return companyID, clientID, id, nil
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
	companyID, clientID, id, err := parseAddressPath(path)
	if err != nil {
		return nil, err
	}
	client, err := b.client.GetClient(ctx, companyID, clientID)
	if err != nil {
		return nil, err
	}
	contacts, err := b.client.GetContacts(ctx, companyID, clientID)
	if err != nil {
		return nil, err
	}
	if id == 0 {
		if client.Professional {
			return toAddressObject(clientProToVCard(client, contacts), formatClientPath(companyID, client.ID)), nil
		} else {
			return nil, fmt.Errorf("client not a professional")
		}
	} else {
		for _, contact := range contacts {
			if contact.ID == id {
				return toAddressObject(contactClientToVCard(client, contact), formatContactPath(companyID, clientID, id)), nil
			}
		}
		return nil, fmt.Errorf("contact not found")
	}
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
			contacts, err := b.client.GetContacts(ctx, companyID, client.ID)
			if err != nil {
				return nil, err
			}
			if client.Professional {
				addressObjects = append(
					addressObjects,
					*toAddressObject(clientProToVCard(client, contacts), formatClientPath(companyID, client.ID)),
				)
			}
			for _, contact := range contacts {
				addressObjects = append(
					addressObjects,
					*toAddressObject(contactClientToVCard(client, contact), formatContactPath(companyID, client.ID, contact.ID)),
				)
			}
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

func refreshToken(client *tiime.Client, authorization string, sharedState *SharedState) {
	for {
		time.Sleep(3 * time.Hour)
		err := func() error {
			sharedState.mu.Lock()
			defer sharedState.mu.Unlock()
			err := client.RefreshToken(context.TODO())
			if err != nil {
				delete(sharedState.clients, authorization)
				fmt.Println("Failed to refresh token", err)
				return err
			}
			return nil
		}()
		if err != nil {
			return
		}
	}
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
		go refreshToken(client, authorization, sharedState)
		sharedState.clients[authorization] = client
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
