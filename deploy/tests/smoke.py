import json
import sys
import time
import urllib.request
import urllib.error
base = sys.argv[1]
def request(path, method='GET'):
    req = urllib.request.Request(base + path, method=method, headers={'X-Forwarded-Host': 'evil.example'})
    try:
        with urllib.request.urlopen(req, timeout=5) as response:
            return response.status, response.read()
    except urllib.error.HTTPError as error:
        return error.code, error.read()
for attempt in range(30):
    try:
        status, body = request('/.well-known/ai-transit.json')
        assert status == 200, (status, body)
        break
    except (OSError, AssertionError):
        if attempt == 29:
            raise
        time.sleep(1)
for path in ['/.well-known/ai-transit.json', '/api/public/transit/v1/snapshot']:
    status, body = request(path)
    assert status == 200, (status, body)
    payload = json.loads(body)
    assert payload['schema_version'] == 'ai-transit.v1'
    assert b'evil.example' not in body and b'SECRET-' not in body
    assert request(path, 'POST')[0] == 405
for path in ['/', '/health', '/public/transit', '/api/v1/public/transit/snapshot', '/v1/messages', '/v1/chat/completions', '/api/v1/admin', '/api/v1/auth/login', '/api/v1/payment']:
    assert request(path)[0] == 404, path
print('Container routes and protocol passed.')
