# 조직 관리 구현 계획

> **작업자 안내:** 이 계획을 실행할 때는 `superpowers:subagent-driven-development` 또는 `superpowers:executing-plans`를 사용한다. 각 작업은 체크박스(`- [ ]`) 단위로 진행하고, 테스트가 통과하는 작은 단위마다 커밋한다.

**목표:** 기존 `User.Group`과 전역 관리자 권한을 건드리지 않고, 별도의 `organization` 기반 사용자 관리 기능을 추가한다.

**아키텍처:** 백엔드에 `Organization`, `User.OrganizationId`, `User.OrganizationRole` 중심의 조직 도메인을 추가한다. 조직 권한은 기존 `AdminAuth()`가 아니라 전용 helper와 `/api/organization*` API에서만 검사한다. 백엔드 계약과 권한 테스트를 먼저 고정한 뒤 프론트엔드 타입과 조직 관리자 화면을 붙인다.

**기술 스택:** Go 1.22+, Gin, GORM v2, SQLite/MySQL/PostgreSQL 호환 migration, React 19, TypeScript, Rsbuild, Base UI, Tailwind CSS, Bun.

---

## 파일 구조

- 생성: `model/organization.go`
  - 조직 모델, 조직 상태/역할 상수, 역할 검증 helper, 조직 멤버 관리 helper를 담당한다.
- 수정: `model/user.go`
  - `OrganizationId`, `OrganizationRole` 필드를 추가하고, 안전한 사용자/profile payload에 포함한다.
- 수정: `model/user_cache.go`
  - `UserBase` 캐시에 조직 필드를 포함해 인증 이후 추가 DB 조회 없이 조직 권한을 판단할 수 있게 한다.
- 수정: `model/main.go`
  - 일반 migration과 fast migration에 `Organization`을 포함한다.
- 생성: `controller/organization.go`
  - root 조직 생성, 조직 멤버 배정, 현재 조직 사용자 조회/상세/제한 수정 API를 담당한다.
- 생성: `controller/organization_test.go`
  - in-memory SQLite 기반으로 조직 권한과 API 동작을 검증한다.
- 수정: `router/api-router.go`
  - `/api/organizations` root 전용 route와 `/api/organization` 현재 조직 route를 추가한다.
- 수정: `controller/user.go`
  - login/self 응답에 조직 필드를 포함한다.
- 생성: `web/default/src/lib/organization-roles.ts`
  - 프론트엔드 조직 역할 상수와 권한 helper를 제공한다.
- 수정: `web/default/src/features/profile/types.ts`
  - 인증 사용자 profile 타입에 조직 필드를 추가한다.
- 생성: `web/default/src/features/organizations/*`
  - 조직 사용자 API, 타입, 테이블 UI를 추가한다.
- 생성: `web/default/src/routes/_authenticated/organization/index.tsx`
  - 조직 관리자 화면 route를 추가한다.
- 수정: `web/default/src/hooks/use-sidebar-data.ts`
  - 전역 admin 그룹과 별도로 조직 navigation 항목을 추가한다.
- 수정: `web/default/src/hooks/use-sidebar-view.ts`
  - 전역 admin 그룹은 기존 전역 role로, 조직 그룹은 조직 role로 각각 필터링한다.
- 수정: `web/default/src/i18n/static-keys.ts`
- 수정: `web/default/src/i18n/locales/{en,zh,fr,ru,ja,vi}.json`
  - 새 UI 문구를 모든 지원 언어에 추가한다.

---

## 작업 1: 조직 모델과 사용자 필드 추가

**파일**

- 생성: `model/organization.go`
- 생성: `model/organization_test.go`
- 수정: `model/user.go`
- 수정: `model/user_cache.go`
- 수정: `model/main.go`

- [ ] **1. 실패하는 모델 테스트 작성**

`model/organization_test.go`를 만들고 다음 동작을 검증한다.

- `member`, `admin`, `owner`는 유효한 조직 역할이다.
- 빈 문자열이나 `root` 같은 값은 유효하지 않은 조직 역할이다.
- `CreateOrganization(name, description, ownerUserId)`는 조직을 만들고 owner 사용자의 `organization_id`, `organization_role`을 갱신한다.
- 조직 owner/admin은 같은 조직의 일반 사용자만 관리할 수 있다.
- 조직 owner/admin은 다른 조직 사용자, 전역 admin, root 사용자를 관리할 수 없다.

실행:

```bash
go test ./model -run 'TestOrganization' -count=1
```

예상 결과: `Organization`, 조직 역할 상수, `CreateOrganization`, `CanManageOrganizationTarget`이 아직 없어서 실패한다.

- [ ] **2. 조직 모델 구현**

`model/organization.go`에 다음 책임을 구현한다.

- `OrganizationStatusEnabled = 1`
- `OrganizationStatusDisabled = 2`
- `OrganizationRoleMember = "member"`
- `OrganizationRoleAdmin = "admin"`
- `OrganizationRoleOwner = "owner"`
- `Organization` struct
  - `Id`
  - `Name`
  - `Description`
  - `OwnerUserId`
  - `Status`
  - `CreatedAt`
  - `UpdatedAt`
- `IsValidOrganizationRole(role string) bool`
- `HasOrganizationAdminRole(role string) bool`
- `HasOrganizationOwnerRole(role string) bool`
- `CreateOrganization(name string, description string, ownerUserId int) (*Organization, error)`
- `CanManageOrganizationTarget(actor User, target User) bool`

`CreateOrganization`은 GORM transaction을 사용한다. 조직 생성과 owner 사용자 업데이트가 하나의 transaction 안에서 성공해야 한다.

- [ ] **3. User 모델 확장**

`model/user.go`의 `User` struct에서 `Group` 근처에 다음 필드를 추가한다.

```go
OrganizationId   int    `json:"organization_id" gorm:"type:int;default:0;column:organization_id;index"`
OrganizationRole string `json:"organization_role" gorm:"type:varchar(16);default:'';column:organization_role"`
```

`User.ToBaseUser()`에도 `OrganizationId`, `OrganizationRole`을 포함한다.

- [ ] **4. UserBase 캐시 확장**

`model/user_cache.go`의 `UserBase`에 다음 필드를 추가한다.

```go
OrganizationId   int    `json:"organization_id"`
OrganizationRole string `json:"organization_role"`
```

- [ ] **5. migration에 Organization 추가**

`model/main.go`의 일반 `DB.AutoMigrate(...)` 목록에 `&Organization{}`을 추가한다.

fast migration의 `migrations` 목록에도 다음 항목을 추가한다.

```go
{&Organization{}, "Organization"}
```

- [ ] **6. 모델 테스트 실행**

```bash
go test ./model -run 'TestOrganization' -count=1
```

예상 결과: 통과.

- [ ] **7. 커밋**

```bash
git add model/organization.go model/organization_test.go model/user.go model/user_cache.go model/main.go
git commit -m "feat: add organization model"
```

---

## 작업 2: 조직 API와 권한 테스트 추가

**파일**

- 생성: `controller/organization.go`
- 생성: `controller/organization_test.go`
- 수정: `router/api-router.go`

- [ ] **1. 실패하는 controller 테스트 작성**

`controller/organization_test.go`를 만들고 in-memory SQLite로 다음 동작을 검증한다.

- root 사용자는 조직을 생성할 수 있다.
- 조직 생성 시 지정한 owner 사용자는 해당 조직의 `owner`가 된다.
- 조직 admin은 다른 조직 사용자를 수정할 수 없다.
- 조직 admin은 같은 조직의 일반 사용자 quota/status/remark를 수정할 수 있다.
- 조직 admin은 전역 admin/root 사용자를 수정할 수 없다.

실행:

```bash
go test ./controller -run 'TestOrganization' -count=1
```

예상 결과: `CreateOrganization`, `UpdateOrganizationUser` 같은 controller 함수가 아직 없어 실패한다.

- [ ] **2. root 조직 생성 endpoint 구현**

`controller/organization.go`에 다음 request 타입과 handler를 추가한다.

- `createOrganizationRequest`
  - `name`
  - `description`
  - `owner_user_id`
- `CreateOrganization(c *gin.Context)`

규칙:

- `c.GetInt("role") == common.RoleRootUser`인 사용자만 허용한다.
- JSON decode는 프로젝트 규칙에 맞게 `common.DecodeJson`을 사용한다.
- 성공 시 `common.ApiSuccess(c, org)`로 응답한다.

- [ ] **3. 조직 사용자 제한 수정 endpoint 구현**

`controller/organization.go`에 다음 request 타입과 handler를 추가한다.

- `updateOrganizationUserRequest`
  - `status?: int`
  - `quota?: int`
  - `remark?: string`
- `UpdateOrganizationUser(c *gin.Context)`

규칙:

- actor는 `model.GetUserById(c.GetInt("id"), false)`로 조회한다.
- actor의 `OrganizationRole`이 `admin` 또는 `owner`여야 한다.
- 대상 사용자는 `model.CanManageOrganizationTarget(actor, target)`을 통과해야 한다.
- 수정 가능한 필드는 `status`, `quota`, `remark`만 허용한다.
- 수정 후 `model.InvalidateUserCache(target.Id)`를 호출한다.

- [ ] **4. 조직 사용자 목록/상세 endpoint 구현**

`controller/organization.go`에 다음 handler를 추가한다.

- `ListOrganizationUsers(c *gin.Context)`
- `GetOrganizationUser(c *gin.Context)`

목록 조회 규칙:

- actor가 조직 admin/owner여야 한다.
- `organization_id = actor.OrganizationId`로 제한한다.
- `role < common.RoleAdminUser` 조건으로 전역 admin/root 사용자를 제외한다.
- pagination은 `common.GetPageQuery(c)`를 사용한다.

상세 조회 규칙:

- 대상 사용자가 같은 조직이어야 한다.
- 전역 admin/root는 제외한다.

- [ ] **5. route 추가**

`router/api-router.go`에 다음 route를 추가한다.

```go
organizationsRoute := apiRouter.Group("/organizations")
organizationsRoute.Use(middleware.RootAuth())
{
	organizationsRoute.POST("/", controller.CreateOrganization)
}

organizationRoute := apiRouter.Group("/organization")
organizationRoute.Use(middleware.UserAuth())
{
	organizationRoute.GET("/users", controller.ListOrganizationUsers)
	organizationRoute.GET("/users/:id", controller.GetOrganizationUser)
	organizationRoute.PATCH("/users/:id", controller.UpdateOrganizationUser)
}
```

- [ ] **6. controller 테스트 실행**

```bash
go test ./controller -run 'TestOrganization' -count=1
```

예상 결과: 통과.

- [ ] **7. 커밋**

```bash
git add controller/organization.go controller/organization_test.go router/api-router.go
git commit -m "feat: add organization user APIs"
```

---

## 작업 3: 조직 owner 멤버십 관리 추가

**파일**

- 수정: `controller/organization.go`
- 수정: `controller/organization_test.go`
- 수정: `router/api-router.go`

- [ ] **1. 실패하는 owner 멤버십 테스트 작성**

`controller/organization_test.go`에 다음 테스트를 추가한다.

- 조직 owner는 일반 사용자를 자기 조직에 추가하고 `organization_role`을 지정할 수 있다.
- 조직 admin은 멤버십을 변경할 수 없다.
- owner도 전역 admin/root 사용자는 조직에 배정하거나 역할을 바꿀 수 없다.
- 유효하지 않은 조직 역할은 거부된다.

실행:

```bash
go test ./controller -run 'TestOrganizationOwner|TestOrganizationAdminCannotAssign' -count=1
```

예상 결과: `AssignOrganizationUser`가 없어 실패한다.

- [ ] **2. owner 멤버십 endpoint 구현**

`controller/organization.go`에 다음 request 타입과 handler를 추가한다.

- `assignOrganizationUserRequest`
  - `organization_role`
- `AssignOrganizationUser(c *gin.Context)`

규칙:

- actor는 조직 `owner`여야 한다.
- actor의 `OrganizationId`가 0이면 거부한다.
- 대상 사용자의 전역 `Role`이 `common.RoleAdminUser` 이상이면 거부한다.
- `organization_role`은 `model.IsValidOrganizationRole`을 통과해야 한다.
- 대상 사용자의 `organization_id`, `organization_role`만 수정한다.
- 수정 후 대상 사용자 cache를 무효화한다.

- [ ] **3. 멤버십 route 추가**

`router/api-router.go`의 `/api/organization` route에 다음 항목을 추가한다.

```go
organizationRoute.PUT("/users/:id/membership", controller.AssignOrganizationUser)
```

- [ ] **4. controller 조직 테스트 실행**

```bash
go test ./controller -run 'TestOrganization' -count=1
```

예상 결과: 통과.

- [ ] **5. 커밋**

```bash
git add controller/organization.go controller/organization_test.go router/api-router.go
git commit -m "feat: add organization membership management"
```

---

## 작업 4: 인증/profile 응답에 조직 필드 노출

**파일**

- 수정: `controller/user.go`
- 생성: `web/default/src/lib/organization-roles.ts`
- 수정: `web/default/src/features/profile/types.ts`

- [ ] **1. 프론트엔드 조직 역할 helper 추가**

`web/default/src/lib/organization-roles.ts`를 생성한다.

```ts
export const ORGANIZATION_ROLE = {
  MEMBER: 'member',
  ADMIN: 'admin',
  OWNER: 'owner',
} as const

export type OrganizationRole =
  (typeof ORGANIZATION_ROLE)[keyof typeof ORGANIZATION_ROLE]

export function hasOrganizationAdminRole(role?: string): boolean {
  return role === ORGANIZATION_ROLE.ADMIN || role === ORGANIZATION_ROLE.OWNER
}

export function hasOrganizationOwnerRole(role?: string): boolean {
  return role === ORGANIZATION_ROLE.OWNER
}
```

- [ ] **2. profile 타입 확장**

`web/default/src/features/profile/types.ts`의 `UserProfile`에서 `group` 다음에 다음 필드를 추가한다.

```ts
/** Organization ID, 0 means no organization */
organization_id?: number
/** Organization role: member, admin, owner */
organization_role?: string
```

- [ ] **3. login 응답 확장**

`controller/user.go`의 `setupLogin` 응답 map에 다음 필드를 추가한다.

```go
"organization_id":   user.OrganizationId,
"organization_role": user.OrganizationRole,
```

- [ ] **4. self/profile 응답 확장**

`controller/user.go`의 `GetSelf` 응답 map에도 다음 필드를 추가한다.

```go
"organization_id":   user.OrganizationId,
"organization_role": user.OrganizationRole,
```

- [ ] **5. 검증**

```bash
go test ./controller -run 'TestOrganization' -count=1
cd web/default && bun run build
```

예상 결과: 둘 다 통과.

- [ ] **6. 커밋**

```bash
git add controller/user.go web/default/src/features/profile/types.ts web/default/src/lib/organization-roles.ts
git commit -m "feat: expose organization profile fields"
```

---

## 작업 5: 조직 관리자 프론트엔드 화면 추가

**파일**

- 생성: `web/default/src/features/organizations/api.ts`
- 생성: `web/default/src/features/organizations/types.ts`
- 생성: `web/default/src/features/organizations/components/organization-users-table.tsx`
- 생성: `web/default/src/routes/_authenticated/organization/index.tsx`
- 수정: `web/default/src/hooks/use-sidebar-data.ts`
- 수정: `web/default/src/hooks/use-sidebar-view.ts`
- 수정: `web/default/src/i18n/static-keys.ts`
- 수정: `web/default/src/i18n/locales/{en,zh,fr,ru,ja,vi}.json`

- [ ] **1. 조직 API client 추가**

`web/default/src/features/organizations/api.ts`를 생성한다.

필요한 함수:

- `getOrganizationUsers({ page, size })`
  - `GET /api/organization/users`
- `updateOrganizationUser(userId, payload)`
  - `PATCH /api/organization/users/:id`

payload 타입은 `status`, `quota`, `remark`만 허용한다.

- [ ] **2. 조직 프론트엔드 타입 추가**

`web/default/src/features/organizations/types.ts`를 생성한다.

필요한 타입:

- `OrganizationRole = 'member' | 'admin' | 'owner'`
- `OrganizationUser`
  - `id`
  - `username`
  - `display_name`
  - `role`
  - `status`
  - `quota`
  - `used_quota`
  - `group`
  - `organization_id`
  - `organization_role`
  - `remark?`
- `OrganizationUsersResponse`
- `OrganizationUserUpdatePayload`

- [ ] **3. 조직 사용자 테이블 추가**

`web/default/src/features/organizations/components/organization-users-table.tsx`를 생성한다.

요구사항:

- 첫 진입 시 조직 사용자 목록을 가져온다.
- 사용자 이름, 조직 역할, 상태, quota를 표시한다.
- quota input의 blur 시 변경된 quota를 저장한다.
- enable/disable 버튼으로 status를 토글한다.
- 실패 시 `toast.error`, 성공 시 `toast.success`를 사용한다.
- 기존 UI 컴포넌트인 `Button`, `Input`을 사용한다.
- 화면 안내문으로 기능 설명을 길게 넣지 않는다.

- [ ] **4. 조직 route 추가**

`web/default/src/routes/_authenticated/organization/index.tsx`를 생성한다.

규칙:

- `createFileRoute('/_authenticated/organization/')`를 사용한다.
- `beforeLoad`에서 `hasOrganizationAdminRole(context.auth.user?.organization_role)`를 확인한다.
- 권한이 없으면 `/`로 redirect한다.
- component는 `OrganizationUsersTable`을 렌더링한다.

- [ ] **5. sidebar navigation 추가**

`web/default/src/hooks/use-sidebar-data.ts`에 `Building2` icon import를 추가한다.

기존 `admin` group 앞에 다음 조직 group을 추가한다.

```tsx
{
  id: 'organization',
  title: t('Organization'),
  items: [
    {
      title: t('Organization Users'),
      url: '/organization',
      icon: Building2,
    },
  ],
}
```

`web/default/src/hooks/use-sidebar-view.ts`에 다음 import를 추가한다.

```ts
import { hasOrganizationAdminRole } from '@/lib/organization-roles'
```

root group filter는 다음 규칙을 따른다.

- `group.id === 'admin'`이면 기존처럼 `role >= ROLE.ADMIN`일 때만 표시한다.
- `group.id === 'organization'`이면 `hasOrganizationAdminRole(user.organization_role)`일 때만 표시한다.
- 그 외 group은 기존처럼 표시한다.

- [ ] **6. i18n 키 추가**

`web/default/src/i18n/static-keys.ts`와 모든 locale JSON에 다음 키를 추가한다.

영어:

```json
{
  "Organization": "Organization",
  "Organization Users": "Organization Users",
  "Failed to load organization users": "Failed to load organization users",
  "Organization user updated": "Organization user updated",
  "Failed to update organization user": "Failed to update organization user"
}
```

한국어 UI는 현재 기본 지원 locale에 없으므로, 한국어 문구가 필요한 경우 별도 ko/kr locale 추가 정책을 먼저 확인한다.

다른 지원 언어 번역:

- zh
  - `Organization`: `组织`
  - `Organization Users`: `组织用户`
  - `Failed to load organization users`: `加载组织用户失败`
  - `Organization user updated`: `组织用户已更新`
  - `Failed to update organization user`: `更新组织用户失败`
- fr
  - `Organization`: `Organisation`
  - `Organization Users`: `Utilisateurs de l'organisation`
  - `Failed to load organization users`: `Impossible de charger les utilisateurs de l'organisation`
  - `Organization user updated`: `Utilisateur de l'organisation mis à jour`
  - `Failed to update organization user`: `Impossible de mettre à jour l'utilisateur de l'organisation`
- ru
  - `Organization`: `Организация`
  - `Organization Users`: `Пользователи организации`
  - `Failed to load organization users`: `Не удалось загрузить пользователей организации`
  - `Organization user updated`: `Пользователь организации обновлен`
  - `Failed to update organization user`: `Не удалось обновить пользователя организации`
- ja
  - `Organization`: `組織`
  - `Organization Users`: `組織ユーザー`
  - `Failed to load organization users`: `組織ユーザーを読み込めませんでした`
  - `Organization user updated`: `組織ユーザーを更新しました`
  - `Failed to update organization user`: `組織ユーザーを更新できませんでした`
- vi
  - `Organization`: `Tổ chức`
  - `Organization Users`: `Người dùng tổ chức`
  - `Failed to load organization users`: `Không thể tải người dùng tổ chức`
  - `Organization user updated`: `Đã cập nhật người dùng tổ chức`
  - `Failed to update organization user`: `Không thể cập nhật người dùng tổ chức`

프론트엔드 i18n 작업 시 프로젝트 스킬 `i18n-translate`를 사용한다.

- [ ] **7. 프론트엔드 빌드**

```bash
cd web/default && bun run build
```

예상 결과: TypeScript 오류 없이 빌드 통과.

- [ ] **8. 커밋**

```bash
git add web/default/src/features/organizations web/default/src/routes/_authenticated/organization web/default/src/hooks/use-sidebar-data.ts web/default/src/hooks/use-sidebar-view.ts web/default/src/lib/organization-roles.ts web/default/src/i18n
git commit -m "feat: add organization admin UI"
```

---

## 작업 6: 전체 검증

**파일**

- 작업 1-5에서 변경된 모든 파일

- [ ] **1. 백엔드 집중 테스트**

```bash
go test ./model ./controller -run 'TestOrganization' -count=1
```

예상 결과: 통과.

- [ ] **2. 변경 패키지 백엔드 테스트**

```bash
go test ./model ./controller ./middleware -count=1
```

예상 결과: 통과.

- [ ] **3. 프론트엔드 빌드**

```bash
cd web/default && bun run build
```

예상 결과: 통과.

- [ ] **4. 수동 API smoke test**

로컬 서버를 실행한 뒤 다음 흐름을 확인한다.

- root로 로그인한다.
- 조직을 생성한다.
- owner 사용자를 지정한다.
- owner로 로그인한다.
- owner가 `/api/organization/users`를 조회할 수 있다.
- owner가 일반 사용자를 자기 조직에 추가할 수 있다.
- 조직 admin이 같은 조직 일반 사용자의 quota/status를 수정할 수 있다.
- 조직 admin이 전역 admin 사용자를 수정할 수 없다.
- 조직 권한만 있는 사용자에게 전역 admin UI가 보이지 않는다.

- [ ] **5. 최종 상태 확인**

```bash
git status --short
```

예상 결과: 의도하지 않은 미커밋 파일이 없다.

---

## 자체 리뷰

스펙 반영 여부:

- 기존 `Group`과 분리된 조직 도메인: 작업 1
- 조직 역할 `member`, `admin`, `owner`: 작업 1, 작업 3
- root 전용 조직 생성: 작업 2
- 조직 전용 API: 작업 2, 작업 3
- 조직 admin의 제한 사용자 관리: 작업 2
- owner의 멤버십 관리: 작업 3
- 프론트엔드 조직 권한 검사: 작업 4, 작업 5
- 기존 전역 admin 화면 보호 유지: 작업 5
- DB 호환성: 작업 1에서 GORM migration과 단순 int/string 컬럼 사용
- 테스트: 작업 1, 작업 2, 작업 3, 작업 6

타입 일관성:

- Go 필드명: `OrganizationId`, `OrganizationRole`
- JSON 필드명: `organization_id`, `organization_role`
- DB 컬럼명: `organization_id`, `organization_role`
- 조직 역할 문자열: `member`, `admin`, `owner`

범위 제한:

- 첫 버전에서는 조직 단위 결제, 조직 API key, 조직 초대, 다중 조직 멤버십, 전체 RBAC 재설계는 구현하지 않는다.
