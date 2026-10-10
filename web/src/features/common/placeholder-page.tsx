import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"

export function PlaceholderPage({ title }: { title: string }) {
  return (
    <Card className="flex-1">
      <CardHeader>
        <CardTitle>{title}</CardTitle>
        <CardDescription>该模块将在后续里程碑中实现。</CardDescription>
      </CardHeader>
      <CardContent className="min-h-[60vh] rounded-xl bg-muted/50" />
    </Card>
  )
}
