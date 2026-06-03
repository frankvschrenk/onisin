# Permissions

Every domain ships its own permission matrix. The matrix gates
both client-side affordances (the toolbar's `save` and `delete`
buttons) and server-side mutations (oosgql's `/mutation`
endpoint). The same block of declarations governs both.

## Declaring permissions

Inside a `domain` block:

```
permission <role> <action> [, <action>]*
```

| Action   | Effect                                                                                            |
| -------- | ------------------------------------------------------------------------------------------------- |
| `read`   | The role may load rows of this domain through any view bound to it.                               |
| `write`  | The role may insert and update rows. Implies `read`.                                              |
| `delete` | The role may remove rows. Implies `read`.                                                         |

Roles are arbitrary identifiers chosen by the application. The
only convention is that they match the values your auth layer
attaches to the `X-OOS-Group` request header.

```
permission admin   read, write, delete
permission manager read, write
permission user    read
```

A role that is not listed for a domain has no access to it. The
absence of an entry is the deny.

## Where the matrix is enforced

Two layers consult the same matrix.

### Client-side (oosd renderer)

When a view is bound to a domain, the renderer reads the matrix
to decide which toolbar items to show:

- `save` is hidden when the active role lacks `write`.
- `delete` is hidden when the active role lacks `delete`.
- `new "..." -> ...` toolbar entries that ultimately invoke
  `write` on a sibling domain are hidden when the role lacks
  `write` on that target.

This is convenience, not security. The same checks happen on the
server.

### Server-side (oosgql)

`oosgql` resolves the active role from the `X-OOS-Group` header
on every request. Its `/mutation` endpoint:

1. Parses the GraphQL document and finds the first selection.
2. Maps the field name's verb to a `PermissionAction`:
   - `update_<domain>`, `insert_<domain>` → `write`
   - `delete_<domain>` → `delete`
3. Calls `assertActionAllowed(domain, action, role)`.
4. Returns a normalised `{"error":"..."}` shape with HTTP 401 if
   no role was supplied or HTTP 403 if the role exists but lacks
   the required action.

`/query` is gated identically against `read`.

## Examples

### A read-only role

A `support` role that can view but not change anything:

```
permission support read
```

If the support user opens a detail view, the toolbar will be
empty (no `save`, no `delete`). If they craft a mutation by hand
and POST it to oosgql with `X-OOS-Group: support`, they get HTTP
403 with `{"error":"action 'write' not allowed for role 'support'"}`.

### A self-service role

An `account_holder` role that may write its own profile but not
delete it:

```
permission account_holder read, write
```

`save` is visible; `delete` is not. Mutations of `update_person`
succeed; `delete_person` returns 403.

### Granting all actions to a role

```
permission admin read, write, delete
```

Every action allowed. The order does not matter; the parser
collects them into a set.

## Common pitfalls

**Forgetting `read`.** Granting `write` without `read` is legal
syntactically but practically useless: the user cannot load the
row they are supposed to save. Always grant `read` to any role
you intend to give `write` or `delete`.

**Roles in the editor.** The editor connects to oosp/oosgql with
the `X-OOS-Group` header set to whichever role the connect bar
is configured for. To exercise a non-admin role's view of the
permissions matrix, change the role on the connect bar before
opening the view.

**Permissions are per-domain, not per-field.** The DSL has no
field-level permissions. To make a single column read-only for
some roles, mark the field `readonly` (which applies to all
roles) or split the data across two domains with different
permission sets and join them via a relation.

## See also

- **[Domain DSL — Permission](domain.md#permission)** — the
  syntax in context.
- **[Quick Start](quickstart.md)** — example of a three-role
  matrix on the `note` domain.
