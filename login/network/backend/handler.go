package backend

import (
	"bytes"
	"fmt"
	"github.com/Blackrush/gofus/login/db"
	"github.com/Blackrush/gofus/protocol/backend"
	frontend "github.com/Blackrush/gofus/protocol/frontend/types"
	"log"
)

type Realm struct {
	frontend.RealmServer
	Address string
	Port    uint16

	client         *Client
	conn_callbacks map[string]chan bool // ticket -> callback
}

func NewRealm(client *Client) *Realm {
	realm := new(Realm)
	realm.client = client
	realm.conn_callbacks = make(map[string]chan bool)
	return realm
}

func (realm *Realm) NotifyUserConnection(ticket string, user *db.User) (callback chan bool) {
	if !realm.Joinable {
		panic(fmt.Sprintf("realm %d is not joinable", realm.Id))
	}

	realm.client.Send(&backend.ClientConnMsg{
		Ticket: ticket,
		User: backend.UserInfos{
			Id:              uint64(user.Id),
			SecretQuestion:  user.SecretQuestion,
			SecretAnswer:    user.SecretAnswer,
			SubscriptionEnd: user.SubscriptionEnd,
			Rights:          user.Rights,
		},
	})

	callback = make(chan bool, 1)
	realm.conn_callbacks[ticket] = callback
	return
}

func client_connection(ctx *context, client *Client) {
	client.Send(&backend.HelloConnectMsg{client.salt})
}

func client_disconnection(ctx *context, client *Client) {
	if client.realm != nil {
		log.Printf("[realm-%02d] is now offline", client.realm.Id)

		client.realm.State = frontend.RealmOfflineState
		client.realm.Joinable = false
		client.realm = nil
	}
}

func client_handle_data(ctx *context, client *Client, arg backend.Message) {
	switch msg := arg.(type) {
	case *backend.AuthReqMsg:
		client_handle_auth(ctx, client, msg)
	case *backend.SetInfosMsg:
		client_handle_set_infos(ctx, client, msg)
	case *backend.SetStateMsg:
		client_handle_set_state(ctx, client, msg)
	case *backend.ClientConnReadyMsg:
		client_handle_client_conn_ready(ctx, client, msg)
	}
}

func client_authenticate(ctx *context, client *Client, credentials []byte) (*Realm, bool) {
	if bytes.Equal(ctx.get_password_hash(client.salt), credentials) {
		return NewRealm(client), true
	}
	return nil, false
}

func client_handle_auth(ctx *context, client *Client, msg *backend.AuthReqMsg) {
	if client.realm != nil {
		log.Printf("[realm-%02d] tried to reauth", client.realm.Id)
		return
	}

	if _, exists := ctx.realms[int(msg.Id)]; exists {
		goto failure
	} else if realm, ok := client_authenticate(ctx, client, msg.Credentials); ok {
		realm.Id = int(msg.Id)
		ctx.realms[realm.Id] = realm
		client.realm = realm

		client.Send(&backend.AuthRespMsg{Success: true})

		log.Printf("[realm-%02d] is now synchronized", client.realm.Id)
		return
	}

failure: // maybe there is a better way to do
	client.Send(&backend.AuthRespMsg{Success: false})
	client.Close()
}

func client_handle_set_infos(ctx *context, client *Client, msg *backend.SetInfosMsg) {
	client.realm.Address = msg.Address
	client.realm.Port = msg.Port
	client.realm.Completion = int(msg.Completion)

	log.Printf("[realm-%02d] updated his infos", client.realm.Id)
}

func client_handle_set_state(ctx *context, client *Client, msg *backend.SetStateMsg) {
	client.realm.State = msg.State

	log.Printf("[realm-%02d] updated his state, now %d", client.realm.Id, client.realm.State)
}

func client_handle_client_conn_ready(ctx *context, client *Client, msg *backend.ClientConnReadyMsg) {
	if callback, ok := client.realm.conn_callbacks[msg.Ticket]; ok {
		callback <- true
		delete(client.realm.conn_callbacks, msg.Ticket)
	} else {
		log.Printf("[realm-%02d] tried to allow a unknown client connection", client.realm.Id)
	}
}
