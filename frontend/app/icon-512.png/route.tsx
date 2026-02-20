import { ImageResponse } from "next/og"

export const runtime = "edge"

export async function GET() {
  return new ImageResponse(
    (
      <div
        style={{
          width: "100%",
          height: "100%",
          display: "flex",
          alignItems: "center",
          justifyContent: "center",
          backgroundColor: "#107E72",
          borderRadius: "96px",
        }}
      >
        <span
          style={{
            fontSize: "310px",
            fontWeight: 800,
            color: "#FFFFFF",
            fontFamily: "system-ui, sans-serif",
          }}
        >
          LR
        </span>
      </div>
    ),
    { width: 512, height: 512 },
  )
}
