package transport

import (
	"errors"
	"io"
	"net"
	"net/rpc"
	"sync"
)

// ReconnectingClient wraps the agent RPC client and redials once when the
// underlying connection is down. net/rpc permanently shuts a client down
// after any stream error (agent restart, poisoned gob frame), which would
// otherwise sever the panel from the agent until the panel itself restarts.
//
// ReconnectingClient, agent RPC istemcisini sarar ve alttaki bağlantı
// koptuğunda bir kez yeniden bağlanır. net/rpc, herhangi bir akış hatasından
// sonra (agent yeniden başlaması, bozulmuş gob çerçevesi) istemciyi kalıcı
// olarak kapatır; bu da paneli, kendisi yeniden başlatılana dek agent'sız
// bırakırdı.
type ReconnectingClient struct {
	mu     sync.Mutex
	client *rpc.Client
}

func NewReconnectingClient(c *rpc.Client) *ReconnectingClient {
	return &ReconnectingClient{client: c}
}

// Call mirrors rpc.Client.Call. The first failing call still returns its real
// error (honesty over silent retries); only calls that find the connection
// already dead trigger a redial and one retry.
//
// Call, rpc.Client.Call ile aynıdır. İlk başarısız çağrı gerçek hatasını
// döndürmeye devam eder (sessiz tekrar yerine dürüstlük); yalnızca bağlantıyı
// zaten ölü bulan çağrılar yeniden bağlanma ve tek bir tekrar tetikler.
func (r *ReconnectingClient) Call(serviceMethod string, args any, reply any) error {
	r.mu.Lock()
	c := r.client
	r.mu.Unlock()

	err := c.Call(serviceMethod, args, reply)
	if err == nil || !connDown(err) {
		return err
	}

	nc, derr := ConnectAgent()
	if derr != nil {
		// The agent is truly unreachable; the original error stands.
		// Agent gerçekten ulaşılamaz; asıl hata geçerli kalır.
		return err
	}

	r.mu.Lock()
	if r.client == c {
		_ = r.client.Close()
		r.client = nc
	} else {
		// Another caller already reconnected; use theirs.
		// Başka bir çağıran zaten yeniden bağlandı; onunkini kullan.
		_ = nc.Close()
		nc = r.client
	}
	r.mu.Unlock()

	return nc.Call(serviceMethod, args, reply)
}

// connDown reports whether the error means the RPC connection itself is dead
// (as opposed to a server-side error for this one call).
// connDown, hatanın (bu tek çağrıya özgü sunucu hatası değil) RPC
// bağlantısının kendisinin öldüğü anlamına gelip gelmediğini bildirir.
func connDown(err error) bool {
	if errors.Is(err, rpc.ErrShutdown) || errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return true
	}
	var opErr *net.OpError
	return errors.As(err, &opErr)
}
