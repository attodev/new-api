# 기능 확인 스크립트 (develop 시험 환경)

`docs/manuals/functional-test-runbook.ko.md`의 1~8절을 자동화한 것. 운영 DB 사본으로 띄운 시험 서버(compose)에 대고 돌린다. 운영 서버에는 절대 돌리지 않는다(잔액을 바꾸고, 앱·Redis를 재시작한다).

```bash
cd scripts/functional-test
cp env.example.sh env.sh && $EDITOR env.sh     # BASE_URL, ADMIN_PAT, USER_ID, USER_KEY, MODEL, COMPOSE_DIR …
./01-billing-consistency.sh
./02-overspend.sh
./03-residency.sh
./04-shutdown-flush.sh      # 앱을 멈췄다 다시 띄운다
./05-redis-restart.sh       # Redis를 재시작한다
MODELS="gpt-4o-mini gemini-2.5-flash" CLAUDE_MODELS="claude-opus-4-7" RESPONSES_MODELS="gpt-5" ./06-channel-smoke.sh
./07-auth.sh
USER_PAT=nap_... ./08-bounds.sh   # USER_PAT가 없으면 교환 적립은 수동
```

필요: `curl`, `jq`, 시험 compose가 있는 호스트(`docker compose exec`로 psql·redis-cli를 쓴다). 각 스크립트는 `PASS:`/`FAIL:` 줄을 내고 실패가 있으면 1로 끝난다.
