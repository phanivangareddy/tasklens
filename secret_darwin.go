package main

/*
#cgo LDFLAGS: -framework Security -framework CoreFoundation
#include <stdlib.h>
#include <string.h>
#include <Security/Security.h>
#include <CoreFoundation/CoreFoundation.h>

static CFStringRef tl_str(const char *s) {
	return CFStringCreateWithCString(NULL, s, kCFStringEncodingUTF8);
}

static CFMutableDictionaryRef tl_query(const char *svc, const char *acct) {
	CFMutableDictionaryRef q = CFDictionaryCreateMutable(NULL, 0,
		&kCFTypeDictionaryKeyCallBacks, &kCFTypeDictionaryValueCallBacks);
	CFStringRef s = tl_str(svc), a = tl_str(acct);
	CFDictionarySetValue(q, kSecClass, kSecClassGenericPassword);
	CFDictionarySetValue(q, kSecAttrService, s);
	CFDictionarySetValue(q, kSecAttrAccount, a);
	CFRelease(s);
	CFRelease(a);
	return q;
}

static int tl_set(const char *svc, const char *acct, const void *pw, int n) {
	CFMutableDictionaryRef q = tl_query(svc, acct);
	SecItemDelete(q);
	CFDataRef d = CFDataCreate(NULL, pw, n);
	CFStringRef label = tl_str(svc);
	CFDictionarySetValue(q, kSecValueData, d);
	CFDictionarySetValue(q, kSecAttrLabel, label);
	OSStatus st = SecItemAdd(q, NULL);
	CFRelease(label);
	CFRelease(d);
	CFRelease(q);
	return (int)st;
}

static int tl_get(const char *svc, const char *acct, void **out, int *n) {
	CFMutableDictionaryRef q = tl_query(svc, acct);
	CFDictionarySetValue(q, kSecReturnData, kCFBooleanTrue);
	CFDictionarySetValue(q, kSecMatchLimit, kSecMatchLimitOne);
	CFTypeRef res = NULL;
	OSStatus st = SecItemCopyMatching(q, &res);
	CFRelease(q);
	if (st != errSecSuccess) return (int)st;
	CFDataRef d = (CFDataRef)res;
	*n = (int)CFDataGetLength(d);
	*out = malloc(*n > 0 ? *n : 1);
	memcpy(*out, CFDataGetBytePtr(d), *n);
	CFRelease(res);
	return 0;
}

static int tl_del(const char *svc, const char *acct) {
	CFMutableDictionaryRef q = tl_query(svc, acct);
	OSStatus st = SecItemDelete(q);
	CFRelease(q);
	return (int)st;
}
*/
import "C"

import (
	"fmt"
	"unsafe"

	"github.com/zalando/go-keyring"
)

// On macOS the password is saved with the Keychain API by TaskLens itself, so
// the Keychain trusts only TaskLens: any other program (including the
// `security` command) gets a macOS prompt instead of the password.
// Versions up to 1.3.0 saved it through `security`, which let any program read
// it silently; those entries are moved over the first time they're used.

func secretSet(service, user, pw string) error {
	keyring.Delete(service, user) // an old-style entry, if any
	s, u := C.CString(service), C.CString(user)
	defer C.free(unsafe.Pointer(s))
	defer C.free(unsafe.Pointer(u))
	b := []byte(pw)
	var p unsafe.Pointer
	if len(b) > 0 {
		p = C.CBytes(b)
		defer C.free(p)
	}
	if st := C.tl_set(s, u, p, C.int(len(b))); st != 0 {
		return fmt.Errorf("the Keychain refused to save the password (error %d)", int(st))
	}
	return nil
}

func secretGet(service, user string) (string, error) {
	s, u := C.CString(service), C.CString(user)
	defer C.free(unsafe.Pointer(s))
	defer C.free(unsafe.Pointer(u))
	var out unsafe.Pointer
	var n C.int
	if st := C.tl_get(s, u, &out, &n); st == 0 {
		defer C.free(out)
		return C.GoStringN((*C.char)(out), n), nil
	}
	// Move an entry saved by an older version over to the safer kind.
	if old, err := keyring.Get(service, user); err == nil {
		secretSet(service, user, old) // also removes the old entry
		return old, nil
	}
	return "", fmt.Errorf("no password saved for %s", user)
}

func secretDelete(service, user string) {
	keyring.Delete(service, user)
	s, u := C.CString(service), C.CString(user)
	defer C.free(unsafe.Pointer(s))
	defer C.free(unsafe.Pointer(u))
	C.tl_del(s, u)
}
