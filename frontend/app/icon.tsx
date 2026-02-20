import { ImageResponse } from "next/og"

export const size = { width: 32, height: 32 }
export const contentType = "image/png"

export default function Icon() {
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
          borderRadius: "6px",
        }}
      >
        <span
          style={{
            fontSize: "20px",
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
