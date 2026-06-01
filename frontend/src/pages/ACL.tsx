import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import {
  usePLCs,
  useACLRules,
  useCreateACLRule,
  useUpdateACLRule,
  useDeleteACLRule,
  type ACLRule,
  type PLCTag,
} from "@/hooks/useApi";
import { ApiError } from "@/lib/api";

const ROLES = ["admin", "operator", "viewer"] as const;
type Role = (typeof ROLES)[number];

interface TagKey {
  plc: string;
  tag: PLCTag;
}

// Build a lookup map: "role|plc|tag" → ACLRule for fast cell resolution.
function buildRuleMap(rules: ACLRule[]): Map<string, ACLRule> {
  const map = new Map<string, ACLRule>();
  for (const r of rules) {
    map.set(`${r.role}|${r.plc}|${r.tag}`, r);
  }
  return map;
}

interface CellProps {
  role: Role;
  plc: string;
  tagName: string;
  rule: ACLRule | undefined;
}

function ACLCell({ role, plc, tagName, rule }: CellProps) {
  const createMutation = useCreateACLRule();
  const updateMutation = useUpdateACLRule();
  const deleteMutation = useDeleteACLRule();

  const isChecked = rule?.allow_write === true;
  const isPending =
    createMutation.isPending ||
    updateMutation.isPending ||
    deleteMutation.isPending;

  function handleChange(checked: boolean) {
    if (checked) {
      // unchecked → checked
      if (!rule) {
        // No rule exists — create with allow_write=true
        createMutation.mutate({ role, plc, tag: tagName, allow_write: true });
      } else {
        // Rule exists with allow_write=false — update to true
        updateMutation.mutate({
          id: rule.id,
          input: { role, plc, tag: tagName, allow_write: true },
        });
      }
    } else {
      // checked → unchecked — DELETE the rule for a clean matrix
      if (rule) {
        deleteMutation.mutate(rule.id);
      }
    }
  }

  return (
    <TableCell className="text-center">
      <input
        type="checkbox"
        checked={isChecked}
        disabled={isPending}
        onChange={(e) => handleChange(e.target.checked)}
        className="h-4 w-4 cursor-pointer disabled:cursor-not-allowed disabled:opacity-50"
        aria-label={`Allow ${role} to write ${plc}/${tagName}`}
      />
    </TableCell>
  );
}

export function ACL() {
  const plcsQuery = usePLCs();
  const aclQuery = useACLRules();

  const plcsError = plcsQuery.error;
  const aclError = aclQuery.error;

  const isLoading = plcsQuery.isLoading || aclQuery.isLoading;
  const isError = plcsQuery.isError || aclQuery.isError;

  // Flatten PLCs → (plc, tag) pairs
  const tagKeys: TagKey[] = [];
  if (plcsQuery.data) {
    for (const plc of plcsQuery.data.data) {
      for (const tag of plc.tags) {
        tagKeys.push({ plc: plc.name, tag });
      }
    }
  }

  const rules = aclQuery.data?.data ?? [];
  const ruleMap = buildRuleMap(rules);

  const unavailable =
    (plcsError instanceof ApiError &&
      (plcsError.status === 404 || plcsError.status === 503)) ||
    (aclError instanceof ApiError &&
      (aclError.status === 404 || aclError.status === 503));

  return (
    <div className="space-y-6">
      <header>
        <h1 className="text-2xl font-semibold">Write Permissions</h1>
        <p className="text-sm text-muted-foreground">
          Role × tag write-permission matrix. Each cell grants a role HTTP write
          access to a specific tag. Admin only.
        </p>
      </header>

      <Card>
        <CardHeader>
          <CardTitle>ACL matrix</CardTitle>
          <CardDescription>
            Check a cell to allow that role to write the tag. Uncheck to revoke.
            Changes take effect immediately.
          </CardDescription>
        </CardHeader>
        <CardContent>
          {unavailable ? (
            <p className="text-sm text-muted-foreground">
              ACL or PLC management API is not available on this gateway build.
            </p>
          ) : isLoading ? (
            <p className="text-sm text-muted-foreground">Loading…</p>
          ) : isError ? (
            <p className="text-sm text-destructive">
              {plcsError?.message ?? aclError?.message ?? "Failed to load data."}
            </p>
          ) : tagKeys.length === 0 ? (
            <p className="text-sm text-muted-foreground">
              No tags to govern yet. Add PLCs with tags first.
            </p>
          ) : (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>PLC</TableHead>
                  <TableHead>Tag</TableHead>
                  {ROLES.map((role) => (
                    <TableHead key={role} className="text-center capitalize">
                      {role}
                    </TableHead>
                  ))}
                </TableRow>
              </TableHeader>
              <TableBody>
                {tagKeys.map(({ plc, tag }) => (
                  <TableRow key={`${plc}|${tag.name}`}>
                    <TableCell className="font-medium">{plc}</TableCell>
                    <TableCell className="font-mono text-xs">
                      {tag.name}
                      {!tag.writable && (
                        <span
                          className="ml-1 text-[10px] text-muted-foreground"
                          title="Master switch off — HTTP writes will be denied regardless of ACL"
                        >
                          (read-only)
                        </span>
                      )}
                    </TableCell>
                    {ROLES.map((role) => (
                      <ACLCell
                        key={role}
                        role={role}
                        plc={plc}
                        tagName={tag.name}
                        rule={ruleMap.get(`${role}|${plc}|${tag.name}`)}
                      />
                    ))}
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          )}
        </CardContent>
      </Card>
    </div>
  );
}
