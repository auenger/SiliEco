/**
 * @vitest-environment jsdom
 */
import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { TableTaskSearch } from "./table-view";

describe("TableTaskSearch", () => {
  it("forwards typing and exposes an accessible clear action", () => {
    const onChange = vi.fn();
    const view = render(
      <TableTaskSearch
        value=""
        onChange={onChange}
        placeholder="Search title or task ID…"
        clearLabel="Clear search"
      />,
    );

    fireEvent.change(screen.getByRole("searchbox"), {
      target: { value: "SILI-4797" },
    });
    expect(onChange).toHaveBeenLastCalledWith("SILI-4797");

    view.rerender(
      <TableTaskSearch
        value="SILI-4797"
        onChange={onChange}
        placeholder="Search title or task ID…"
        clearLabel="Clear search"
      />,
    );
    fireEvent.click(screen.getByRole("button", { name: "Clear search" }));
    expect(onChange).toHaveBeenLastCalledWith("");
  });
});
