import { describe, it, expect } from "vitest"

import { isDeployFieldRequired, targetSchema } from "./deploy-targets-page"

// 防漂移测试：Label 红星的必填规则（isDeployFieldRequired）必须与 zod 校验
// （targetSchema.superRefine）完全一致——否则会出现"标了红星却不校验"或
// "校验必填却没标红星"的分裂。两者已共用同一函数，本测试锁定该契约。

const TYPES = ["local", "ssh", "agent", "k8s", "webhook"]
const SCENES = ["", "generic", "nginx", "apache", "tomcat", "iis"]
const FIELDS = [
  "name", "host", "user", "private_key", "pfx_path", "iis_bindings",
  "cert_path", "agent_id", "secret_name", "url",
]

// 全空提交时 zod 报错的字段集合（private_key 与 password 二选一，皆空才报）
function schemaRequiredFields(type: string, scene: string): Set<string> {
  const hasScene = ["local", "ssh", "agent"].includes(type)
  const res = targetSchema.safeParse({
    type,
    // 无场景通道不能传 scene（enum 会先失败，superRefine 被跳过）
    scene: hasScene ? scene || "generic" : undefined,
    ...emptyValues(),
  })
  if (res.success) return new Set()
  return new Set(res.error.issues.map((i) => String(i.path[0])))
}

function emptyValues(): Record<string, string> {
  return {
    name: "", host: "", user: "", private_key: "", password: "", pfx_path: "",
    iis_bindings: "", cert_path: "", agent_id: "", secret_name: "", url: "", port: "",
  }
}

// 归一化 scene：无场景通道（k8s/webhook）恒为 ""
function normScene(type: string, scene: string): string {
  return ["local", "ssh", "agent"].includes(type) ? scene || "generic" : ""
}

describe("isDeployFieldRequired 与 zod 校验一致", () => {
  for (const type of TYPES) {
    for (const scene of SCENES) {
      it(`${type}/${scene || "无场景"}`, () => {
        const s = normScene(type, scene)
        const fromSchema = schemaRequiredFields(type, scene)
        for (const f of FIELDS) {
          const marked = isDeployFieldRequired(type, s, f)
          const validated = fromSchema.has(f)
          expect(marked, `字段 ${f} 在 ${type}/${s} 下：红星=${marked} 校验=${validated}`).toBe(validated)
        }
      })
    }
  }

  it("必填字段集合非空（防止规则被误清空）", () => {
    const all = TYPES.flatMap((t) => SCENES.map((sc) => FIELDS.filter((f) => isDeployFieldRequired(t, normScene(t, sc), f))))
    expect(all.some((fs) => fs.length > 0)).toBe(true)
    // name 恒必填
    expect(isDeployFieldRequired("local", "generic", "name")).toBe(true)
  })
})
