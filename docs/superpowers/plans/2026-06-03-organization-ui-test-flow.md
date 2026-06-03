# 조직 UI 테스트 흐름 구현 계획

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**목표:** 브라우저 UI만으로 조직 생성, 조직 멤버 배정, 조직 사용자 관리를 모두 테스트할 수 있게 만든다.

**아키텍처:** 기존 `Organization Users` 화면을 조직 관리 콘솔로 확장한다. root는 조직 생성 폼을 사용하고, 조직 owner는 멤버 배정 폼을 사용하며, 조직 owner/admin은 기존 사용자 목록 관리 기능을 그대로 사용한다.

**기술 스택:** React 19, TypeScript, Rsbuild, Base UI 기반 공용 UI 컴포넌트, 기존 `/api/organizations`, `/api/organization/users`, `/api/user` API.

---

### 작업 1: 조직 프론트 API 확장

**파일:**
- 수정: `web/default/src/features/organizations/api.ts`
- 수정: `web/default/src/features/organizations/types.ts`

- [ ] `createOrganization`, `assignOrganizationUser` API 함수를 추가한다.
- [ ] 조직 생성 요청/응답과 멤버 배정 요청 타입을 추가한다.

### 작업 2: 조직 관리 화면 확장

**파일:**
- 수정: `web/default/src/features/organizations/components/organization-users-table.tsx`

- [ ] root 사용자에게 조직 생성 폼을 표시한다.
- [ ] owner 사용자에게 조직 멤버 배정 폼을 표시한다.
- [ ] 사용자 선택은 기존 admin 사용자 목록 API를 사용해 최근 일반 사용자 목록을 불러온다.
- [ ] 성공 시 toast를 표시하고 조직 사용자 목록을 새로고침한다.

### 작업 3: 접근성과 탐색 보강

**파일:**
- 수정: `web/default/src/hooks/use-sidebar-view.ts`
- 수정: `web/default/src/i18n/static-keys.ts`
- 수정: `web/default/src/i18n/locales/*.json`

- [ ] root 사용자에게도 `Organization` 메뉴를 보여준다.
- [ ] 새 UI 문구를 i18n static key와 locale 파일에 추가한다.

### 작업 4: 검증

**명령:**
- `bun run build` (`web/default`)
- `curl -s http://localhost:3001/api/status`

- [ ] 프론트 빌드가 통과하는지 확인한다.
- [ ] 실행 중인 프론트 dev 서버가 변경을 반영하는지 확인한다.
