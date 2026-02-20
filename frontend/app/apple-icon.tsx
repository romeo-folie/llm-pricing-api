import { ImageResponse } from "next/og"

export const size = { width: 180, height: 180 }
export const contentType = "image/png"

export default function AppleIcon() {
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
          borderRadius: "36px",
        }}
      >
        <span
          style={{
            fontSize: "110px",
            fontWeight: 800,
            color: "#FFFFFF",
            fontFamily: "system-ui, sans-serif",
          }}
        >
          L
        </span>
      </div>
    ),
    { ...size },
  )
}
