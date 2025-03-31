#!/usr/bin/env python3
# -*- coding: utf-8 -*-

import unittest
import sys
import os
import importlib.util

class TestMainScript(unittest.TestCase):
    """Testy pre hlavný skript aplikácie."""
    
    def test_use_zone_center(self):
        """Test na overenie hodnoty USE_ZONE_CENTER v skripte."""
        # Načítame hlavný skript ako modul
        spec = importlib.util.spec_from_file_location("main", "python/main.py")
        main_module = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(main_module)
        
        # Overíme hodnotu USE_ZONE_CENTER
        use_zone_center = getattr(main_module, "USE_ZONE_CENTER", None)
        self.assertIsNotNone(use_zone_center, "Konštanta USE_ZONE_CENTER nebola nájdená")
        
        # Vypíšeme aktuálnu hodnotu
        print(f"Aktuálna hodnota USE_ZONE_CENTER: {use_zone_center}")
        
        # Ak je nastavený parameter --expect-true alebo --expect-false, overíme očakávanú hodnotu
        if "--expect-true" in sys.argv:
            self.assertTrue(use_zone_center, "USE_ZONE_CENTER má byť True")
        elif "--expect-false" in sys.argv:
            self.assertFalse(use_zone_center, "USE_ZONE_CENTER má byť False")

if __name__ == "__main__":
    unittest.main(argv=[sys.argv[0]]) 