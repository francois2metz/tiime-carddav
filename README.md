# Tiime CardDav server

Expose [Tiime][] clients as vcard/carddav server. Authentication is done with your Tiime credentials sent via basic auth.
Tiime's companies are listed as addressbooks. Contacts are readonly for now.

Tested with:
- [DAVx5](https://www.davx5.com/)
- [vdirsyncer](https://github.com/pimutils/vdirsyncer?tab=readme-ov-file)

## Usage

    make
    ./server

The address of the server is then http://127.0.0.1:1234/me/

## License

AGPL v3

[Tiime]: https://www.tiime.fr/
