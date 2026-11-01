#!/usr/bin/env bash
# fork: O13 — Open WebUI 결합 테스트 스택 초기화 (원본: OpenWebUI 브랜치, develop 로그인 세션·step-up 증명에 맞춤)
#
# new-api 컨테이너가 뜬 뒤 한 번 실행한다. 여러 번 실행해도 안전하다(멱등).
#
#   - 초기 설정(루트 계정) 수행
#   - 서비스 계정 + 서비스 토큰 생성 → 토큰 키를 .env 의 OWUI_SERVICE_KEY 에 기록
#   - relay(openwebui.*)·OIDC(oauth2.*) 옵션 주입, ServerAddress 를 http://PUBLIC_HOST:PORT 로 설정
#   - NEW_API_TEST_USERS 의 테스트 사용자 생성 + 이메일 설정(psql)
#   - OWUI_MANUAL_LINKS 가 있으면 step-up 증명을 받아 Open WebUI 계정을 수동 연결
#   - MOCK_UPSTREAM=1 이면 mock-gpt 채널과 가격, 테스트 사용자 잔액까지 넣는다
#
# develop 의 관리 API 호출 방식:
#   - POST /api/user/login 응답의 data.access_token 을 Authorization: Bearer 로 보낸다 (쿠키 세션 아님).
#     로그인이 2단계 인증·패스키 확인을 요구하면(data 에 access_token 이 없음) 중단한다.
#   - 일반 사용자(역할 1) 생성·옵션 저장·채널 생성·잔액 조정은 증명 없이 된다. Open WebUI 수동 연결은
#     POST /api/verify (method=password, scope=admin.user.update, context={user_id}) 로 받은 proof_token 을
#     X-Security-Proof 로 보낸다 (한 번 쓰면 소모된다).
#   - 이메일은 넣는 관리 API 가 없어 psql 로 넣는다. 잔액은 develop 이 Redis 에 상주시키므로(O12) psql 대신
#     POST /api/user/manage (add_quota, override) 로 넣는다.
#
# 요구: bash, python3(표준 라이브러리만), docker compose (이메일은 postgres 컨테이너의 psql 로 넣는다)
set -euo pipefail
cd "$(dirname "$0")"
if [ ! -f .env ]; then
  echo ".env 가 없습니다. cp .env.example .env 후 값을 채우세요." >&2
  exit 1
fi
set -a
# shellcheck disable=SC1091
. ./.env
set +a
export NEW_API_URL="${NEW_API_URL:-http://localhost:${NEW_API_PORT:-3000}}"

python3 - <<'PY'
import json, os, subprocess, sys, time, urllib.error, urllib.parse, urllib.request

B = os.environ["NEW_API_URL"].rstrip("/")
E = os.environ
PUBLIC_BASE = f"http://{E['PUBLIC_HOST']}:{E.get('NEW_API_PORT','3000')}"
OWUI_BASE = f"http://{E['PUBLIC_HOST']}:{E.get('OPEN_WEBUI_PORT','8080')}"

def log(msg): print(f"[bootstrap] {msg}", flush=True)
def die(msg): print(f"[bootstrap] 실패: {msg}", file=sys.stderr); sys.exit(1)
def env_bool(name, default):
    v = E.get(name, "").strip().lower()
    return default if v == "" else v in ("1", "true", "yes", "on")

def page_items(data):
    # develop 목록 API 는 {page, page_size, total, items} 를 돌려준다.
    if isinstance(data, dict):
        return data.get("items") or []
    return data or []

class Client:
    """Bearer 액세스 토큰(develop 로그인 세션)으로 관리 API 를 부른다. 쿠키는 쓰지 않는다."""
    def __init__(self, token=None, user_id=None):
        self.token = token
        self.user_id = user_id
    def call(self, method, path, body=None, headers=None):
        data = None
        h = dict(headers or {})
        if body is not None:
            data = json.dumps(body).encode(); h["Content-Type"] = "application/json"
        if self.token:
            h["Authorization"] = f"Bearer {self.token}"
        req = urllib.request.Request(B + path, data=data, method=method, headers=h)
        try:
            with urllib.request.urlopen(req, timeout=20) as r:
                text = r.read().decode(); status = r.status
        except urllib.error.HTTPError as e:
            text = e.read().decode(); status = e.code
        try: return status, json.loads(text)
        except json.JSONDecodeError: return status, {"success": False, "message": text[:200]}

def login(user, pw):
    st, j = Client().call("POST", "/api/user/login", {"username": user, "password": pw})
    if not j.get("success"):
        return None
    d = j.get("data") or {}
    if not d.get("access_token"):
        die(f"{user} 로그인이 추가 확인(2단계 인증·패스키)을 요구합니다. 시험 스택에서는 이 계정의 2단계 인증·패스키를 끄세요")
    return Client(d["access_token"], d["user"]["id"])

def logout(client):
    # 이 스크립트가 만든 로그인 세션을 남기지 않는다 (실패해도 무시).
    try: client.call("POST", "/api/user/auth/logout")
    except Exception: pass

# 1. 기동 대기
for i in range(60):
    try:
        st, j = Client().call("GET", "/api/status")
        if j.get("success"): break
    except Exception: pass
    time.sleep(2)
else:
    die(f"{B} 가 응답하지 않습니다")
log(f"new-api 응답 확인: {B}")

# 2. 초기 설정 (develop: 루트 이름 최대 12자, 비밀번호 8~128자)
st, setup = Client().call("GET", "/api/setup")
if not setup["data"]["status"]:
    st, j = Client().call("POST", "/api/setup", {
        "username": E["NEW_API_ROOT_USER"], "password": E["NEW_API_ROOT_PASSWORD"],
        "confirmPassword": E["NEW_API_ROOT_PASSWORD"], "SelfUseModeEnabled": False, "DemoSiteEnabled": False})
    if not j.get("success"): die(f"setup: {j.get('message')}")
    log("초기 설정 완료 (루트 생성)")
else:
    log("초기 설정은 이미 되어 있음")

root = login(E["NEW_API_ROOT_USER"], E["NEW_API_ROOT_PASSWORD"])
if root is None: die("루트 로그인 실패 (.env 의 NEW_API_ROOT_USER / NEW_API_ROOT_PASSWORD 확인)")

def find_user(username):
    st, j = root.call("GET", f"/api/user/search?keyword={urllib.parse.quote(username)}&p=1&page_size=100")
    if not j.get("success"): die(f"사용자 검색 {username}: {j.get('message')}")
    return next((u for u in page_items(j.get("data")) if u.get("username") == username), None)

def ensure_user(username, password, display):
    """일반 사용자(역할 1)를 만든다. 역할 1 은 step-up 증명이 필요 없다(관리자 이상 생성만 admin.user.create)."""
    u = find_user(username)
    if u is not None:
        return u["id"]
    st, j = root.call("POST", "/api/user/", {"username": username, "password": password,
                                              "display_name": display, "role": 1})
    if not j.get("success"): die(f"사용자 {username} 생성: {j.get('message')}")
    u = find_user(username)
    if u is None: die(f"사용자 {username} 생성 뒤 조회 실패")
    log(f"사용자 생성: {username}")
    return u["id"]

try:
    # 3. 서비스 계정 + 토큰 (토큰은 서비스 계정 본인 세션으로 만든다)
    ensure_user(E["NEW_API_SERVICE_USER"], E["NEW_API_SERVICE_PASSWORD"], "Open WebUI service")
    svc = login(E["NEW_API_SERVICE_USER"], E["NEW_API_SERVICE_PASSWORD"])
    if svc is None: die(f"{E['NEW_API_SERVICE_USER']} 로그인 실패 (.env 의 NEW_API_SERVICE_PASSWORD 확인)")
    try:
        def find_service_token():
            st, j = svc.call("GET", "/api/token/?p=1&page_size=100")
            if not j.get("success"): die(f"토큰 목록: {j.get('message')}")
            return next((t for t in page_items(j.get("data")) if t.get("name") == "openwebui-service"), None)
        tok = find_service_token()
        if tok is None:
            st, j = svc.call("POST", "/api/token/", {"name": "openwebui-service", "unlimited_quota": True,
                                                     "expired_time": -1, "remain_quota": 0})
            if not j.get("success"): die(f"서비스 토큰 생성: {j.get('message')}")
            tok = find_service_token()
            if tok is None: die("서비스 토큰 생성 뒤 조회 실패")
            log("서비스 토큰 생성: openwebui-service (쿼터 무제한, 만료 없음)")
        st, j = svc.call("POST", f"/api/token/{tok['id']}/key")
        if not j.get("success"): die(f"서비스 토큰 키 조회: {j.get('message')}")
        key = j["data"] if isinstance(j["data"], str) else j["data"]["key"]
    finally:
        logout(svc)

    # .env 에 기록
    lines = open(".env").read().splitlines()
    out, seen = [], False
    for ln in lines:
        if ln.startswith("OWUI_SERVICE_KEY="): out.append(f"OWUI_SERVICE_KEY={key}"); seen = True
        else: out.append(ln)
    if not seen: out.append(f"OWUI_SERVICE_KEY={key}")
    open(".env", "w").write("\n".join(out) + "\n")
    log("OWUI_SERVICE_KEY 를 .env 에 기록")

    # 4. 옵션 (PUT /api/option/ 는 RootAuth. 서명 키 옵션 OIDCSigning*Key 는 이 API 로 쓸 수 없다)
    def opt(k, v):
        st, j = root.call("PUT", "/api/option/", {"key": k, "value": v})
        if not j.get("success"): die(f"옵션 {k}: {j.get('message')}")
    opt("ServerAddress", PUBLIC_BASE)
    # relay: 서비스 키·시크릿을 먼저 넣어야 relay_enabled 가 저장된다
    opt("openwebui.service_api_key", key)
    opt("openwebui.jwt_secret", E["OWUI_JWT_SECRET"])
    opt("openwebui.jwt_issuer", "open-webui")
    opt("openwebui.allow_unsigned_requests", env_bool("OWUI_ALLOW_UNSIGNED", True))
    opt("openwebui.relay_enabled", True)
    # OIDC: client_id·client_secret(16자 이상)·redirect_uri 를 먼저 넣어야 enabled 가 저장된다
    opt("oauth2.client_id", E.get("OIDC_CLIENT_ID", "openwebui"))
    opt("oauth2.client_secret", E["OIDC_CLIENT_SECRET"])
    opt("oauth2.redirect_uri", OWUI_BASE + "/oauth/oidc/login/callback")
    opt("oauth2.app_url", OWUI_BASE)
    opt("oauth2.post_logout_redirect_uri", OWUI_BASE + "/auth")
    opt("oauth2.enabled", True)
    # MCP: API 키 → /oauth2/token-from-key. client_id 는 oauth2.client_id 와 달라야 한다
    opt("oauth2.token_from_key_client_id", E.get("OIDC_MCP_CLIENT_ID", "openwebui-mcp"))
    opt("oauth2.token_from_key_enabled", True)
    log(f"옵션 주입 완료 (issuer={PUBLIC_BASE}, redirect_uri={OWUI_BASE}/oauth/oidc/login/callback)")

    def psql(sql):
        cmd = ["docker", "compose", "exec", "-T", "postgres", "psql", "-U", E.get("PG_USER", "newapi"),
               "-d", E.get("PG_DB", "newapi"), "-v", "ON_ERROR_STOP=1", "-c", sql]
        r = subprocess.run(cmd, capture_output=True, text=True)
        if r.returncode != 0: die(f"psql: {r.stderr.strip()}")
    def sql_str(s):
        return "'" + s.replace("'", "''") + "'"

    # 5. 루트 이메일 (선택) + 테스트 사용자. 관리자 사용자 생성·수정 API 는 이메일을 저장하지 않으므로 psql 로 넣는다.
    #    OIDC 클레임과 relay 자동 연결은 DB 에서 이메일을 읽는다.
    if E.get("NEW_API_ROOT_EMAIL"):
        psql(f"update users set email = {sql_str(E['NEW_API_ROOT_EMAIL'].strip().lower())} "
             f"where username = {sql_str(E['NEW_API_ROOT_USER'])};")
        log(f"루트 이메일 설정: {E['NEW_API_ROOT_EMAIL']}")
    test_users = []
    for spec in [u for u in E.get("NEW_API_TEST_USERS", "").split(",") if u.strip()]:
        name, pw, email = (spec.strip().split(":") + ["", ""])[:3]
        uid = ensure_user(name, pw, name.capitalize())
        test_users.append((name, uid))
        if email:
            psql(f"update users set email = {sql_str(email.strip().lower())} where username = {sql_str(name)};")
            log(f"{name} 이메일 설정: {email}")

    # 6. 수동 연결 (선택): 관리자·루트처럼 자동 연결되지 않는 계정. develop 은 step-up 증명을 요구한다.
    for spec in [s for s in E.get("OWUI_MANUAL_LINKS", "").split(",") if s.strip()]:
        name, _, sub = spec.strip().partition(":")
        sub = sub.strip()
        if not name or not sub: die(f"OWUI_MANUAL_LINKS 형식 오류: {spec!r} (\"사용자 이름:sub\")")
        u = find_user(name)
        if u is None: die(f"수동 연결 대상 {name} 이 없습니다")
        st, j = root.call("GET", f"/api/openwebui/links?user_id={u['id']}")
        if not j.get("success"): die(f"연결 조회 {name}: {j.get('message')}")
        if j.get("data"):
            if j["data"].get("sub") != sub:
                log(f"{name} 은 이미 다른 sub({j['data'].get('sub')}) 에 연결되어 있음 — 사용자 편집 화면에서 해제 후 다시 실행")
            else:
                log(f"{name} 은 이미 {sub} 에 연결되어 있음")
            continue
        st, j = root.call("POST", "/api/verify", {"method": "password", "scope": "admin.user.update",
                                                  "context": {"user_id": u["id"]},
                                                  "password": E["NEW_API_ROOT_PASSWORD"]})
        if not j.get("success"): die(f"step-up 증명({name}): {j.get('message')}")
        proof = j["data"]["proof_token"]
        st, j = root.call("POST", "/api/openwebui/links", {"user_id": u["id"], "sub": sub},
                          headers={"X-Security-Proof": proof})
        if not j.get("success"): die(f"수동 연결 {name}: {j.get('code') or ''} {j.get('message')}")
        log(f"수동 연결: {name} ↔ Open WebUI sub {sub}")

    # 7. mock 업스트림
    if E.get("MOCK_UPSTREAM", "0") == "1":
        st, j = root.call("GET", "/api/channel/?p=1&page_size=100")
        if not j.get("success"): die(f"채널 목록: {j.get('message')}")
        if not any(c.get("name") == "mock-upstream" for c in page_items(j.get("data"))):
            st, j = root.call("POST", "/api/channel/", {"mode": "single", "channel": {
                "type": 1, "name": "mock-upstream", "key": "mock-key", "base_url": "http://mock-upstream:9100",
                "models": "mock-gpt", "group": "default", "status": 1, "priority": 0, "weight": 0}})
            if not j.get("success"): die(f"mock 채널: {j.get('message')}")
            log("mock-upstream 채널 생성 (모델 mock-gpt)")
        st, j = root.call("GET", "/api/option/")
        if not j.get("success"): die(f"옵션 조회: {j.get('message')}")
        opts = {o["key"]: o["value"] for o in j["data"]}
        for k in ("ModelRatio", "CompletionRatio"):
            m = json.loads(opts.get(k) or "{}"); m["mock-gpt"] = 1; opt(k, json.dumps(m))
        for name, uid in test_users:
            st, j = root.call("POST", "/api/user/manage", {"id": uid, "action": "add_quota",
                                                          "mode": "override", "value": 5000000})
            if not j.get("success"): die(f"{name} 잔액 설정: {j.get('message')}")
        log("mock-gpt 가격(비율 1)과 테스트 사용자 잔액(5000000) 설정")
finally:
    logout(root)

log("완료. 다음: docker compose up -d   (Open WebUI 가 .env 의 OWUI_SERVICE_KEY 로 기동)")
log(f"Open WebUI: {OWUI_BASE}   new-api: {PUBLIC_BASE}")
PY
