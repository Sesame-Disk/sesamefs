# OIDC Claims Reference for SesameFS

**Purpose**: This document serves as a reference for OIDC provider implementers on what claims SesameFS expects and how they are processed.

## Required Claims

### `sub` (string, required)
The subject identifier claim uniquely identifies the user across the OIDC provider.

- **Type**: String
- **Required**: Yes
- **Purpose**: Primary key for OIDC-to-user mapping
- **Requirements**: Must be unique and stable across login sessions
- **Example**: `"sub": "user-uuid-1234"`

### `email` (string, required)
The user's email address.

- **Type**: String
- **Required**: Yes
- **Purpose**: User identification and communication
- **Requirements**: Must be a valid email address
- **Example**: `"email": "alice@acme.com"`

### `name` (string, recommended)
The user's display name.

- **Type**: String
- **Required**: Recommended
- **Purpose**: Display name for UI
- **Fallback**: If not provided, SesameFS falls back to `preferred_username`, then to `email`
- **Example**: `"name": "Alice Smith"`

## Organization Claim

The organization claim determines which organization the user belongs to within SesameFS.

### Configuration
- **Configurable via**: `org_claim` (default: none)
- **Behavior**:
  - If configured, the claim value is used as the organization UUID directly
  - Can be mapped to a platform organization via `platform_org_claim_value`
  - If empty or missing, falls back to `default_org_id`

### Example
```json
{
  "tenant_id": "550e8400-e29b-41d4-a716-446655440000"
}
```

Configuration:
```yaml
oidc:
  org_claim: tenant_id
  default_org_id: "550e8400-e29b-41d4-a716-446655440000"
```

## Role Claim

The role claim assigns a platform role to the user.

### Configuration
- **Configurable via**: `roles_claim` (default: none)
- **Accepted formats**: String or array of strings

### Role Mapping Table

| Claim Value | Mapped Role |
|------------|-------------|
| `superadmin`, `super_admin`, `platform_admin` | `superadmin` |
| `admin`, `administrator` | `admin` |
| `user`, `member` | `user` |
| `readonly`, `read-only`, `viewer` | `readonly` |
| `guest`, `external` | `guest` |

### Example
```json
{
  "roles": ["admin"]
}
```

or

```json
{
  "role": "admin"
}
```

## Group Claim

The group claim assigns users to groups within their organization. Groups are used for access control and organizational structure.

### Configuration
- **Configurable via**: `groups_claim` (default: none)
- **Enable sync**: `sync_groups_on_login: true`
- **Full sync mode**: `full_sync_groups: true`

### Supported Formats

#### Array of Strings Format
The simplest format where group IDs and names are identical.

```json
{
  "groups": ["engineering", "design", "product"]
}
```

- Both the group ID and name are set to the string value
- Group UUID is deterministically generated from the ID

#### Array of Objects Format
Rich format supporting separate IDs and display names.

```json
{
  "groups": [
    {"id": "eng-001", "name": "Engineering"},
    {"id": "des-002", "name": "Design Team"},
    {"id": "prod-003", "name": "Product Management"}
  ]
}
```

- `id` (required): External group identifier
- `name` (optional): Human-readable group name (defaults to `id` if not provided)

### Behavior
- Groups are automatically created on first encounter (upsert semantics)
- Group UUIDs are deterministically generated using SHA1-based UUID v5
- Sync occurs on every login when enabled

## Department Claim

The department claim assigns users to departments, which are special groups representing organizational departments.

### Configuration
- **Configurable via**: `departments_claim` (default: none)
- **Enable sync**: `sync_departments_on_login: true`
- **Full sync mode**: `full_sync_departments: true`

### Supported Formats

#### Array of Strings Format
Simple department names.

```json
{
  "departments": ["sales", "marketing", "engineering"]
}
```

#### Array of Objects Format
Rich format supporting hierarchical departments.

```json
{
  "departments": [
    {"id": "sales-001", "name": "Sales Division", "parent_id": "corp-001"},
    {"id": "mktg-002", "name": "Marketing", "parent_id": "corp-001"},
    {"id": "corp-001", "name": "Corporate"}
  ]
}
```

- `id` (required): External department identifier
- `name` (optional): Human-readable department name
- `parent_id` (optional): External ID of parent department for hierarchy

### Behavior
- Departments are groups with `is_department=true` flag
- Support hierarchical structure via `parent_group_id`
- Departments are created with deterministic UUIDs
- Parent departments must exist or be created in the same claim set

## Example Token Payload

Here's a complete example of an ID token with all supported claims:

```json
{
  "iss": "https://accounts.example.com/openid",
  "sub": "user-uuid-1234",
  "aud": "sesamefs-client-id",
  "exp": 1706600000,
  "iat": 1706596400,
  "email": "alice@acme.com",
  "name": "Alice Smith",
  "email_verified": true,
  "preferred_username": "asmith",
  "tenant_id": "550e8400-e29b-41d4-a716-446655440000",
  "roles": ["admin"],
  "groups": [
    {"id": "eng-001", "name": "Engineering"},
    {"id": "design-002", "name": "Design Team"}
  ],
  "departments": [
    {"id": "sales", "name": "Sales Division", "parent_id": "corp"},
    {"id": "corp", "name": "Corporate"}
  ]
}
```

## Configuration Reference

### YAML Configuration Fields

| YAML Field | Env Var | Default | Description |
|-----------|---------|---------|-------------|
| `org_claim` | `OIDC_ORG_CLAIM` | (none) | Claim name containing organization ID |
| `roles_claim` | `OIDC_ROLES_CLAIM` | (none) | Claim name containing user roles |
| `groups_claim` | `OIDC_GROUPS_CLAIM` | (none) | Claim name containing groups |
| `departments_claim` | `OIDC_DEPARTMENTS_CLAIM` | (none) | Claim name containing departments |
| `sync_groups_on_login` | `OIDC_SYNC_GROUPS_ON_LOGIN` | `false` | Enable automatic group sync on login |
| `sync_departments_on_login` | `OIDC_SYNC_DEPARTMENTS_ON_LOGIN` | `false` | Enable automatic department sync on login |
| `full_sync_groups` | `OIDC_FULL_SYNC_GROUPS` | `false` | Remove user from groups not in claims |
| `full_sync_departments` | `OIDC_FULL_SYNC_DEPARTMENTS` | `false` | Remove user from departments not in claims |
| `default_org_id` | `OIDC_DEFAULT_ORG_ID` | (none) | Fallback organization UUID |
| `platform_org_claim_value` | `OIDC_PLATFORM_ORG_CLAIM_VALUE` | (none) | Map specific claim value to platform org |

### Example Configuration

```yaml
oidc:
  issuer_url: "https://accounts.example.com/openid"
  client_id: "sesamefs-client-id"
  client_secret: "${OIDC_CLIENT_SECRET}"
  org_claim: "tenant_id"
  roles_claim: "roles"
  groups_claim: "groups"
  departments_claim: "departments"
  sync_groups_on_login: true
  sync_departments_on_login: true
  full_sync_groups: false
  full_sync_departments: false
  default_org_id: "550e8400-e29b-41d4-a716-446655440000"
```

## Sync Behavior

### Additive Sync (Default)
When `full_sync_groups` or `full_sync_departments` is `false`:

- User is **added** to groups/departments present in claims
- User is **not removed** from existing groups/departments
- Existing memberships are preserved
- Best for environments where groups are managed both via OIDC and manually

### Full Sync
When `full_sync_groups` or `full_sync_departments` is `true`:

- User is **added** to groups/departments present in claims
- User is **removed** from groups/departments not in claims
- OIDC becomes the single source of truth for group membership
- Best for environments where OIDC provider fully manages group membership

### Sync Timing
- Synchronization occurs on **every successful login**
- Groups and departments are created automatically if they don't exist (upsert semantics)
- Changes take effect immediately after login completes

### Example Scenarios

#### Scenario 1: Additive Sync
```
Initial state: User is member of ["engineering", "design"]
OIDC claims: ["engineering", "product"]
Result: User is member of ["engineering", "design", "product"]
```

#### Scenario 2: Full Sync
```
Initial state: User is member of ["engineering", "design"]
OIDC claims: ["engineering", "product"]
Result: User is member of ["engineering", "product"]
Note: "design" membership was removed
```

## Deterministic UUID Mapping

SesameFS generates deterministic UUIDs for groups and departments to ensure consistent mapping between external IDs and internal database IDs.

### Algorithm
Uses SHA1-based UUID version 5 (RFC 4122):

```
uuid.NewSHA1(namespace, orgID + ":group:" + externalGroupID)
```

or for departments:

```
uuid.NewSHA1(namespace, orgID + ":dept:" + externalDepartmentID)
```

### Properties
- **Deterministic**: Same external ID always maps to the same internal UUID
- **Organization-scoped**: Different organizations can use the same external ID without collision
- **Type-separated**: Groups and departments with the same external ID don't collide (different prefix)
- **Predictable**: You can pre-compute UUIDs if needed for testing or migration

### Example
```
Organization ID: "550e8400-e29b-41d4-a716-446655440000"
External Group ID: "eng-001"
Input string: "550e8400-e29b-41d4-a716-446655440000:group:eng-001"
Generated UUID: Deterministic SHA1-based UUID v5
```

### Benefits
- Idempotent group creation (same external ID always references the same group)
- No need to maintain external ID mapping tables
- Consistent across deployments and database migrations
- Groups can be referenced before they are created

## Best Practices

### For OIDC Provider Implementers

1. **Use stable subject identifiers**: The `sub` claim should never change for a user
2. **Include email verification**: Set `email_verified` claim when email is verified
3. **Provide rich group metadata**: Use object format for groups to provide meaningful names
4. **Model organizational hierarchy**: Use department `parent_id` to represent org structure
5. **Keep external IDs stable**: Changing group IDs will create new groups in SesameFS
6. **Use meaningful external IDs**: External IDs appear in logs and debugging tools

### For SesameFS Administrators

1. **Choose sync mode carefully**: Use full sync only when OIDC is authoritative
2. **Test with sample tokens**: Validate claim structure before production deployment
3. **Monitor group creation**: Track new groups being created from OIDC claims
4. **Document claim mappings**: Keep internal documentation of which claims map to which fields
5. **Plan for migration**: When changing claim names, coordinate with user communication

## Troubleshooting

### User Not Assigned to Expected Groups
- Verify `sync_groups_on_login: true` is set
- Check that `groups_claim` matches the actual claim name in the token
- Inspect the ID token to confirm groups are present
- Review logs for group creation/sync events

### Groups Not Being Removed
- This is expected behavior with additive sync
- Enable `full_sync_groups: true` for removal of unmatched groups
- Note: Full sync only affects groups managed by OIDC, not manually created memberships

### Department Hierarchy Not Working
- Ensure parent departments are included in the same claim array
- Verify `parent_id` values match department `id` values
- Check that parent departments are created before or alongside child departments

### User Has Wrong Role
- Check `roles_claim` configuration matches token claim name
- Verify role values match the mapping table (case-insensitive)
- Review role claim format (string vs array)
- Confirm user's token actually contains the role claim

## Security Considerations

1. **Validate token signatures**: Always verify OIDC tokens are properly signed
2. **Check audience claims**: Ensure `aud` claim matches your client ID
3. **Verify issuer**: Confirm `iss` matches your configured issuer URL
4. **Respect expiration**: Honor `exp` claim and reject expired tokens
5. **Use HTTPS**: Always use TLS for OIDC communication
6. **Protect client secrets**: Store `client_secret` securely (use environment variables)
7. **Limit claim scope**: Only request claims you actually use
8. **Audit group changes**: Log group membership changes for security review

## Additional Resources

- [OpenID Connect Core 1.0 Specification](https://openid.net/specs/openid-connect-core-1_0.html)
- [RFC 4122: UUID Specification](https://tools.ietf.org/html/rfc4122)
- [JWT Claims Registry](https://www.iana.org/assignments/jwt/jwt.xhtml)

---

**Document Version**: 1.0
**Last Updated**: 2026-02-02
**Maintained By**: SesameFS Team
