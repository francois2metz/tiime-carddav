package main

import (
	"context"
	"golang.org/x/crypto/bcrypt"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"path"

	"github.com/emersion/go-vcard"
	"github.com/emersion/go-webdav"
	"github.com/emersion/go-webdav/carddav"

	tiime "github.com/francois2metz/steampipe-plugin-tiime/tiime/client"
        auth "github.com/abbot/go-http-auth"
)

func clientToVCard(client tiime.Client2) (vcard.Card) {
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

func parseContactPath(p string) (int, error) {
	_, filename := path.Split(p)
	id, err := strconv.Atoi(filename)
	if err != nil {
		return 0, err
	}
	return id, nil
}

func formatContactPath(id int) string {
	return fmt.Sprint("/me/contacts/default/", id)
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

func (*tiimeBackend) ListAddressBooks(ctx context.Context) ([]carddav.AddressBook, error) {
	log.Println("List address books")
	return []carddav.AddressBook{
		carddav.AddressBook{
			Path:            "/me/contacts/default/",
			Name:            "Tiime",
			Description:     "Tiime Contacts",
			MaxResourceSize: 100 * 1024,
		},
	}, nil
}

func (b *tiimeBackend) GetAddressBook(ctx context.Context, path string) (*carddav.AddressBook, error) {
	log.Println("Get address books")
	abs, err := b.ListAddressBooks(ctx)
	if err != nil {
		panic(err)
	}
	for _, ab := range abs {
		if ab.Path == path {
			return &ab, nil
		}
	}
	return nil, webdav.NewHTTPError(404, fmt.Errorf("Not found"))
}

func (b *tiimeBackend) CreateAddressBook(ctx context.Context, ab *carddav.AddressBook) error {
	return fmt.Errorf("not supported")
}

func (*tiimeBackend) DeleteAddressBook(ctx context.Context, path string) error {
	return fmt.Errorf("not supported")
}

func (b *tiimeBackend) GetAddressObject(ctx context.Context, path string, req *carddav.AddressDataRequest) (*carddav.AddressObject, error) {
	log.Println("GetAddressObject", path)
	id, err := parseContactPath(path)
	if err != nil {
		return nil, err
	}
	client, err := b.client.GetClient(ctx, int64(id))
	if err != nil {
		return nil, err
	}
	card := clientToVCard(client)
	addressObject := carddav.AddressObject{
		Path: formatContactPath(client.ID),
		ETag: "1",
		Card: card,
	}
	return &addressObject, nil
}

func (b *tiimeBackend) ListAddressObjects(ctx context.Context, path string, req *carddav.AddressDataRequest) ([]carddav.AddressObject, error) {
	log.Println("List address objects", path)
	opts := tiime.PaginationOpts{Start: 0, End: 100}
	addressObjects := []carddav.AddressObject{}
	for {
		clients, pagination, err := b.client.GetClients(ctx, opts)
		if err != nil {
			return nil, err
		}
		for _, client := range clients {
			card := clientToVCard(client)
			addressObjects = append(addressObjects, carddav.AddressObject{
				Path: formatContactPath(client.ID),
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
	log.Println("Query address objects", path)
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

func main() {
	company_id, err := strconv.Atoi(os.Getenv("TIIME_COMPANY_ID"))
	if err != nil {
		log.Println("TIIME_COMPANY_ID environnement variable error", err)
		return
	}
	config := tiime.ClientConfig{
		Email:     os.Getenv("TIIME_EMAIL"),
		Password:  os.Getenv("TIIME_PASSWORD"),
		CompanyID: company_id,
	}
	client, err := tiime.New(context.TODO(), config)
	if err != nil {
		log.Println("error creating client", err)
		return
	}

	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(os.Getenv("TIIME_PASSWORD")), bcrypt.DefaultCost)
	if err != nil {
		log.Println("error generating password", err)
		return
	}
        authenticator := auth.NewBasicAuthenticator("Tiime", func(user, realm string) string {
		if user == os.Getenv("TIIME_EMAIL") {
			return string(hashedPassword)
		}
		return ""
	})


	s := &http.Server{
		Addr: "0.0.0.0:1234",
		Handler: http.HandlerFunc(func(resp http.ResponseWriter, req *http.Request) {
			log.Println("Request", req.Method, req.URL)
			ctx := authenticator.NewContext(context.Background(), req)
			authInfo := auth.FromContext(ctx)
			authInfo.UpdateHeaders(resp.Header())
			if authInfo == nil || !authInfo.Authenticated {
				http.Error(resp, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
				return
			}

			b := &tiimeBackend{
				client: client,
			}

			h := &carddav.Handler{Backend: b}
			h.ServeHTTP(resp, req)
		}),
	}

	log.Println("CardDAV server listening on", s.Addr)
	s.ListenAndServe()
}
