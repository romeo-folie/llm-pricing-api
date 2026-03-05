"use client"

import { Suspense } from "react"
import { Canvas } from "@react-three/fiber"
import { useGLTF, OrbitControls, Environment, Center } from "@react-three/drei"
import type { CSSProperties } from "react"

// Preload on module evaluation so the browser starts fetching before paint.
useGLTF.preload("/models/llmrates-hero-v1.glb")

// ─── Model ─────────────────────────────────────────────────────────────────────

function Model() {
  const { scene } = useGLTF("/models/llmrates-hero-v1.glb")
  return (
    <Center>
      <primitive object={scene} />
    </Center>
  )
}

// ─── Canvas wrapper (client-only) ──────────────────────────────────────────────

interface HeroModel3DProps {
  className?: string
  style?: CSSProperties
}

/**
 * HeroModel3D — renders the llmrates-hero-v1.glb asset inside a React Three
 * Fiber canvas with:
 *   - Auto-rotate + damp OrbitControls (mouse + touch)
 *   - PBR environment lighting via drei <Environment>
 *   - dpr clamped to [1, 2] so the canvas is never > 2× resolution on HiDPI
 *   - Suspense fallback is null (parent supplies an SSR-rendered SVG fallback)
 */
export default function HeroModel3D({ className, style }: HeroModel3DProps) {
  return (
    <div
      className={className}
      style={{
        width: "100%",
        aspectRatio: "4 / 3",
        minHeight: 260,
        ...style,
      }}
    >
      <Canvas
        dpr={[1, 2]}
        camera={{ position: [0, 1, 5], fov: 42 }}
        gl={{ antialias: true }}
        style={{ width: "100%", height: "100%" }}
      >
        <Suspense fallback={null}>
          {/* Ambient fill */}
          <ambientLight intensity={0.5} />
          {/* Key light */}
          <directionalLight position={[4, 6, 4]} intensity={1.2} castShadow />
          {/* Fill light from opposite side */}
          <directionalLight position={[-4, 2, -4]} intensity={0.4} />

          <Model />

          {/* PBR environment — city preset blends well with dark/light themes */}
          <Environment preset="city" />

          {/* Interaction: auto-rotate slow, allow user orbit, no pan */}
          <OrbitControls
            enablePan={false}
            minPolarAngle={Math.PI / 5}
            maxPolarAngle={Math.PI - Math.PI / 4}
            autoRotate
            autoRotateSpeed={0.6}
            enableDamping
            dampingFactor={0.07}
          />
        </Suspense>
      </Canvas>
    </div>
  )
}
