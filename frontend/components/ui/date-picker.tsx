"use client"

import * as React from "react"
import { format, parseISO } from "date-fns"
import { CalendarIcon } from "lucide-react"

import { cn } from "@/lib/utils"
import { Button } from "@/components/ui/button"
import { Calendar } from "@/components/ui/calendar"
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@/components/ui/popover"

interface DatePickerProps {
  /** ISO date string "YYYY-MM-DD" or empty string */
  value: string
  onChange: (value: string) => void
  placeholder?: string
  className?: string
}

export function DatePicker({
  value,
  onChange,
  placeholder = "Pick a date",
  className,
}: DatePickerProps) {
  const selected = value ? parseISO(value) : undefined

  return (
    <Popover>
      <PopoverTrigger asChild>
        <Button
          variant="outline"
          className={cn("font-outfit text-sm justify-start gap-2", className)}
          style={{
            padding: "6px 12px",
            border: "1px solid var(--border)",
            backgroundColor: "var(--bg)",
            color: value ? "var(--ink)" : "var(--dim)",
            borderRadius: 0,
            fontWeight: 400,
            height: "auto",
          }}
        >
          <CalendarIcon
            size={14}
            style={{ color: "var(--dim)", flexShrink: 0 }}
          />
          {selected ? format(selected, "MMM d, yyyy") : placeholder}
        </Button>
      </PopoverTrigger>
      <PopoverContent
        className="w-auto p-0"
        align="start"
        style={{
          backgroundColor: "var(--surface)",
          border: "1px solid var(--border)",
          borderRadius: 0,
        }}
      >
        <Calendar
          mode="single"
          selected={selected}
          onSelect={(date) => onChange(date ? format(date, "yyyy-MM-dd") : "")}
          disabled={(date) => date > new Date()}
          initialFocus
          style={
            {
              "--cell-size": "2.25rem",
              fontSize: "0.9rem",
            } as React.CSSProperties
          }
        />
      </PopoverContent>
    </Popover>
  )
}
