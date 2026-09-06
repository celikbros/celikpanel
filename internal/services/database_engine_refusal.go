package services

import (
	"errors"
	"strings"

	"github.com/alicelik/celikpanel/internal/hostcmd"
)

// R-053. The panel installs MariaDB or PostgreSQL through its own service
// flow and then cannot use the engine it just installed: it keeps a root
// password for every database server and never sets one, while a freshly
// packaged MariaDB admits root only through the local unix socket and a
// freshly packaged PostgreSQL leaves the postgres role without a password.
// Neither will accept the empty credential the panel holds.
//
// The engine says so clearly - "Access denied for user 'root'", "password
// authentication failed for user" - and the panel used to turn that into a
// 500 with the word "internal" in it. That is the defect this file exists to
// remove: the operator cannot act on "internal server error", and the one
// thing they must do is not a mystery. Whether the product should own the
// engine's root credential is a larger question and is deliberately NOT
// answered here; nothing in this file changes a host.
//
// R-053. Panel MariaDB veya PostgreSQL'i kendi servis akisiyla kurar, sonra
// az once kurdugu motoru kullanamaz: her veritabani sunucusu icin bir root
// parolasi saklar ama hicbir zaman bir tane belirlemez. Motor bunu acikca
// soyler; panel bunu icinde "internal" gecen bir 500'e cevirirdi. Bu dosyanin
// varlik nedeni o kusuru gidermektir. Urunun motorun root kimligini sahiplenip
// sahiplenmeyecegi daha buyuk bir sorudur ve burada YANITLANMAZ; bu dosyadaki
// hicbir sey bir makineyi degistirmez.

// DatabaseEngineRefusal is why an engine would not do what it was asked.
// DatabaseEngineRefusal, bir motorun istenen isi neden yapmadigidir.
type DatabaseEngineRefusal int

const (
	// DatabaseEngineRefusalNone: the failure is not one of the two this file
	// can name. Everything unrecognised lands here, so a new failure mode is
	// reported as itself rather than mislabelled as one of these.
	// DatabaseEngineRefusalNone: basarisizlik bu dosyanin adlandirabildigi iki
	// durumdan biri degil.
	DatabaseEngineRefusalNone DatabaseEngineRefusal = iota
	// DatabaseEngineRefusalCredential: the engine answered, and rejected the
	// credential the panel presented. This is the R-053 case.
	// DatabaseEngineRefusalCredential: motor yanit verdi ve panelin sundugu
	// kimlik bilgisini reddetti.
	DatabaseEngineRefusalCredential
	// DatabaseEngineRefusalUnreachable: nothing answered at the recorded host
	// and port. A different problem with a different remedy, told apart so the
	// operator is not sent to change a password on a server that is down.
	// DatabaseEngineRefusalUnreachable: kayitli adres ve portta yanit veren
	// olmadi.
	DatabaseEngineRefusalUnreachable
)

// Both clients report the same two facts in their own words. The credential
// phrases are checked first: a client that reached the engine and was refused
// has already proved the engine is up, and some of its wording ("connection
// failed") would otherwise read as unreachable.
//
// Iki istemci de ayni iki olguyu kendi sozleriyle bildirir. Once kimlik
// ifadeleri denetlenir: motora ulasip reddedilmis bir istemci motorun ayakta
// oldugunu zaten kanitlamistir.
var databaseCredentialRefusalPhrases = []string{
	// MariaDB / MySQL client, ERROR 1045.
	"access denied for user",
	"error 1045",
	"using password: no",
	"using password: yes",
	// PostgreSQL, SQLSTATE 28P01 and its neighbours.
	"password authentication failed",
	"authentication failed for user",
	"no password supplied",
	"peer authentication failed",
	"ident authentication failed",
	"28p01",
	"28000",
}

var databaseUnreachablePhrases = []string{
	// MariaDB / MySQL client, ERROR 2002 and 2003.
	"can't connect to",
	"cannot connect to",
	"error 2002",
	"error 2003",
	// PostgreSQL and the Go net layer beneath both clients.
	"could not connect",
	"connection refused",
	"connection reset",
	"no such file or directory",
	"host is unreachable",
	"network is unreachable",
	"no route to host",
	"i/o timeout",
	"context deadline exceeded",
}

// DatabaseEngineRefusalError is a failure whose reason was read where the
// reason existed. R-054 found the shape first: nft's stderr was discarded by
// the call that ran it, so the log said "exit status 1" and every layer above
// had to guess. The mysql client does exactly the same thing here, and this is
// the third path to meet it - so the output is classified once, at the only
// place that has it, and the classification travels instead of the text. The
// text is not carried on purpose: a client's diagnostic can echo the statement
// it failed on, and a statement can carry a password.
//
// DatabaseEngineRefusalError, nedeni var oldugu yerde okunmus bir
// basarisizliktir. Metnin kendisi bilerek tasinmaz: bir istemcinin teshis
// metni basarisiz olan ifadeyi yankilayabilir ve bir ifade parola tasiyabilir.
type DatabaseEngineRefusalError struct {
	Refusal DatabaseEngineRefusal
	cause   error
}

func (e *DatabaseEngineRefusalError) Error() string {
	if e.cause == nil {
		return "database engine refusal"
	}
	return e.cause.Error()
}

func (e *DatabaseEngineRefusalError) Unwrap() error { return e.cause }

// WrapDatabaseEngineFailure reads a command's own output once and keeps only
// what it means. An output that says nothing recognisable is returned
// unwrapped, so nothing is claimed that was not read.
//
// It reads through hostcmd.Diagnostic rather than off the output slice alone,
// because the output slice is not the whole of what the command said: a driver
// that calls Output() instead of CombinedOutput() leaves the engine's sentence
// inside *exec.ExitError, where this used to miss it. That is the same reading
// R-054 needed, and it now happens in one place for both.
//
// WrapDatabaseEngineFailure, bir komutun kendi ciktisini bir kez okur ve
// yalnizca ne anlama geldigini saklar. Okumayi hostcmd.Diagnostic uzerinden
// yapar; cunku cikti dilimi komutun soyledigi her sey degildir.
func WrapDatabaseEngineFailure(cause error, output []byte) error {
	if cause == nil {
		return nil
	}
	refusal := classifyDatabaseEngineText(hostcmd.Diagnostic(output, cause))
	if refusal == DatabaseEngineRefusalNone {
		return cause
	}
	return &DatabaseEngineRefusalError{Refusal: refusal, cause: cause}
}

// ClassifyDatabaseEngineRefusal names why an engine operation failed, when it
// failed for one of the two reasons the panel can give an operator an
// instruction about.
//
// ClassifyDatabaseEngineRefusal, bir motor isleminin neden basarisiz
// oldugunu, panelin operatore talimat verebilecegi iki nedenden biriyse,
// adlandirir.
func ClassifyDatabaseEngineRefusal(err error) DatabaseEngineRefusal {
	if err == nil {
		return DatabaseEngineRefusalNone
	}
	// A driver that read its command's output already answered this.
	// Ciktisini okumus bir surucu bunu zaten yanitladi.
	var refusalErr *DatabaseEngineRefusalError
	if errors.As(err, &refusalErr) {
		return refusalErr.Refusal
	}
	return classifyDatabaseEngineText(err.Error())
}

// classifyDatabaseEngineText is the one place the two clients' wording is
// read, whether it arrives as a command's output or as a library error.
// classifyDatabaseEngineText, iki istemcinin sozlerinin okundugu tek yerdir.
func classifyDatabaseEngineText(raw string) DatabaseEngineRefusal {
	text := strings.ToLower(raw)
	for _, phrase := range databaseCredentialRefusalPhrases {
		if strings.Contains(text, phrase) {
			return DatabaseEngineRefusalCredential
		}
	}
	for _, phrase := range databaseUnreachablePhrases {
		if strings.Contains(text, phrase) {
			return DatabaseEngineRefusalUnreachable
		}
	}
	return DatabaseEngineRefusalNone
}

// The sentences. Each names what is true of the host and the one thing to do
// about it, and each names somewhere in CelikPanel to do it.
//
// R-057 rewrote these. They used to say "give this server a root password,
// then have an administrator register the server in CelikPanel again with that
// password" - which was honest about the panel's position and useless as an
// instruction, because the server list is filled by autodiscovery and there
// was no register-a-server screen to follow it to. An instruction with nowhere
// to be carried out is worse than none. The panel now opens an account of its
// own instead, and these sentences point at the place that does it.
//
// They still say what the panel will not do - it does not touch the operator's
// root - because that is the reason there is a separate account at all.
//
// Cumleler. Her biri makine icin neyin dogru oldugunu, panelin kurdugu bir
// sunucuda bunun neden dogru oldugunu ve yapilacak tek seyi adlandirir.
const (
	mariaDBCredentialRefusedMessage = "This MariaDB server is running and " +
		"answering, but it refused the credential CelikPanel holds for it. On " +
		"this server's page, have an administrator open CelikPanel's own " +
		"account on it - CelikPanel creates an account for itself and leaves " +
		"your root password alone - or, if this engine is on another machine, " +
		"give CelikPanel a username and password for it there."

	postgreSQLCredentialRefusedMessage = "This PostgreSQL server is running " +
		"and answering, but it refused the credential CelikPanel holds for it. " +
		"On this server's page, have an administrator open CelikPanel's own " +
		"account on it - CelikPanel creates a role for itself and leaves the " +
		"postgres role alone - or, if this engine is on another machine, give " +
		"CelikPanel a username and password for it there."

	genericCredentialRefusedMessage = "This database server is running and " +
		"answering, but it refused the credential CelikPanel holds for it. On " +
		"this server's page, have an administrator open CelikPanel's own " +
		"account on it, or give CelikPanel a username and password for it."

	databaseUnreachableMessage = "Nothing answered at the address recorded " +
		"for this database server. Start the engine on that server, or " +
		"correct the address and port recorded here, and try again."
)

// DatabaseEngineRefusalMessage is the operator's sentence for a named
// refusal. driverType is the panel's own engine identifier, never operator
// text, so the message stays entirely developer-authored.
//
// DatabaseEngineRefusalMessage, adlandirilmis bir ret icin operatorun
// cumlesidir. driverType panelin kendi motor kimligidir.
func DatabaseEngineRefusalMessage(
	driverType string,
	refusal DatabaseEngineRefusal,
) string {
	switch refusal {
	case DatabaseEngineRefusalUnreachable:
		return databaseUnreachableMessage
	case DatabaseEngineRefusalCredential:
		switch driverType {
		case "mariadb":
			return mariaDBCredentialRefusedMessage
		case "postgresql":
			return postgreSQLCredentialRefusedMessage
		default:
			return genericCredentialRefusedMessage
		}
	default:
		return ""
	}
}
