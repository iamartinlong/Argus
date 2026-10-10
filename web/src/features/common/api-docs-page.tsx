import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"

export function ApiDocsPage() {
  return (
    <Card className="flex-1">
      <CardHeader>
        <CardTitle>API 文档</CardTitle>
      </CardHeader>
      <CardContent className="p-0">
        <iframe
          src="/swagger-ui/"
          title="Swagger UI"
          className="h-[calc(100vh-12rem)] w-full border-0"
        />
      </CardContent>
    </Card>
  )
}
