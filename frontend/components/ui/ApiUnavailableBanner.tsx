/** Yellow warning banner shown when the pricing API is unreachable. Server component. */
export default function ApiUnavailableBanner() {
  return (
    <div
      className="font-outfit text-sm"
      style={{
        padding: "10px 14px",
        marginBottom: "20px",
        border: "1px solid var(--border)",
        borderLeft: "3px solid var(--yellow)",
        backgroundColor: "var(--yellowLt)",
        color: "var(--muted)",
      }}
    >
      ⚠ Pricing API is currently unavailable — data will appear once connectivity is restored.
    </div>
  )
}
