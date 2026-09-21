import * as React from 'react'
import { cn } from '@/lib/utils'

type Variant = 'default' | 'outline' | 'ghost' | 'destructive'
type Size = 'sm' | 'md' | 'icon'

const variants: Record<Variant, string> = {
  default: 'bg-primary text-primary-foreground hover:opacity-90',
  outline: 'border bg-background hover:bg-accent',
  ghost: 'hover:bg-accent',
  destructive: 'bg-[var(--destructive)] text-white hover:opacity-90',
}
const sizes: Record<Size, string> = {
  sm: 'h-8 px-3 text-[12.5px]',
  md: 'h-9 px-4',
  icon: 'h-9 w-9',
}

export interface ButtonProps extends React.ButtonHTMLAttributes<HTMLButtonElement> {
  variant?: Variant
  size?: Size
}

export const Button = React.forwardRef<HTMLButtonElement, ButtonProps>(
  ({ className, variant = 'default', size = 'md', ...props }, ref) => (
    <button
      ref={ref}
      className={cn(
        'inline-flex items-center justify-center gap-1.5 rounded-md font-medium transition-colors',
        'focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--ring)]',
        'disabled:pointer-events-none disabled:opacity-50',
        variants[variant],
        sizes[size],
        className,
      )}
      {...props}
    />
  ),
)
Button.displayName = 'Button'
