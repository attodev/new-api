# 복사해서 env.sh로 저장하고 값을 채운다: cp env.example.sh env.sh
# 모든 스크립트는 같은 디렉터리의 env.sh를 읽는다.

# 시험 서버
export BASE_URL="http://127.0.0.1:3000"

# 관리자(root) 개인 접근 토큰. 설정 → 접근 토큰에서 만든다. 스크립트가 쓰는 권한:
#   user:read, user:write, api_key:read, api_key:write, api_key:reveal, log:read,
#   redemption:write, billing:write (구독 플랜 상한 확인용)
# 가장 단순한 방법은 모든 권한을 가진 토큰 하나를 시험 기간에만 만드는 것이다.
export ADMIN_PAT="nap_..."

# 과금 시험에 쓸 사용자(운영 사용자가 아닌 시험용 계정)와 그 사용자의 API 키(sk-...)
export USER_ID="123"
export USER_KEY="sk-..."

# 조직 과금(09): 조직 멤버가 될 두 번째 시험 사용자와 그 API 키. ORG_ID를 비우면 사용자의 조직을 쓰거나 새로 만든다.
export ORG_USER_ID="124"
export ORG_USER_KEY="sk-..."
export ORG_ID=""
# export ORG_USER_PAT="nap_..."   # 멤버 본인의 접근 토큰(있으면 개인 교환 차단까지 자동 확인)

# 값싼 채팅 모델 하나 (OpenAI 호환 채널). 채널 스모크는 06 스크립트의 MODELS 목록을 따로 쓴다.
export MODEL="gpt-4o-mini"

# docker compose 디렉터리(시험 환경 compose 파일이 있는 곳)와 서비스 이름
export COMPOSE_DIR="/home/molla/newapi-testenv"
export COMPOSE="docker compose"
export APP_SERVICE="newapi"
export PG_SERVICE="postgres"
export REDIS_SERVICE="redis"
export DB_USER="appuser"
export DB_NAME="appdb"

# 배치 반영 간격(초). compose의 BATCH_UPDATE_INTERVAL과 같게.
export BATCH_INTERVAL="5"
