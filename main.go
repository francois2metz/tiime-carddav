package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"

	"github.com/emersion/go-vcard"
	"github.com/emersion/go-webdav"
	"github.com/emersion/go-webdav/carddav"

	tiime "github.com/francois2metz/steampipe-plugin-tiime/tiime/client"
)

type tiimeBackend struct {
	client *tiime.Client
}

func (*tiimeBackend) CurrentUserPrincipal(ctx context.Context) (string, error) {
	return "/me", nil
}

func (*tiimeBackend) AddressBookHomeSetPath(ctx context.Context) (string, error) {
	return "/me/contacts", nil
}

func (*tiimeBackend) ListAddressBooks(ctx context.Context) ([]carddav.AddressBook, error) {
	log.Println("List address books")
	return []carddav.AddressBook{
		carddav.AddressBook{
			Path:            "/me/contacts/default",
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

func (*tiimeBackend) GetAddressObject(ctx context.Context, path string, req *carddav.AddressDataRequest) (*carddav.AddressObject, error) {
	log.Println("GetAddressObject", path)
	return nil, webdav.NewHTTPError(404, fmt.Errorf("Not found"))
}

func (b *tiimeBackend) ListAddressObjects(ctx context.Context, path string, req *carddav.AddressDataRequest) ([]carddav.AddressObject, error) {
	log.Println("List address objects", path)
	return []carddav.AddressObject{}, nil
}

func (b *tiimeBackend) QueryAddressObjects(ctx context.Context, path string, query *carddav.AddressBookQuery) ([]carddav.AddressObject, error) {
	log.Println("Query address objects", path)
	opts := tiime.PaginationOpts{Start: 0, End: 100}
	addressObjects := []carddav.AddressObject{}
	for {
		clients, pagination, err := b.client.GetClients(ctx, opts)
		if err != nil {
			return nil, err
		}
		for _, client := range clients {
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
			addressObjects = append(addressObjects, carddav.AddressObject{
				Path: fmt.Sprint(path, "/", client.ID),
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

func (*tiimeBackend) PutAddressObject(ctx context.Context, path string, card vcard.Card, opts *carddav.PutAddressObjectOptions) (*carddav.AddressObject, error) {
	return nil, fmt.Errorf("not supported")
}

func (*tiimeBackend) DeleteAddressObject(ctx context.Context, path string) error {
	return fmt.Errorf("not supported")
}

func main() {
	company_id, err := strconv.Atoi(os.Getenv("TIIME_COMPANY_ID"))
	if err != nil {
		log.Println("TIIME_COMPANY_ID environnement variable error")
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

	s := &http.Server{
		Addr: "0.0.0.0:1234",
		Handler: http.HandlerFunc(func(resp http.ResponseWriter, req *http.Request) {
			log.Println("Request !")
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
