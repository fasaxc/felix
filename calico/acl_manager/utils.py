# Copyright (c) 2014 Metaswitch Networks
# All Rights Reserved.
#
#    Licensed under the Apache License, Version 2.0 (the "License"); you may
#    not use this file except in compliance with the License. You may obtain
#    a copy of the License at
#
#         http://www.apache.org/licenses/LICENSE-2.0
#
#    Unless required by applicable law or agreed to in writing, software
#    distributed under the License is distributed on an "AS IS" BASIS, WITHOUT
#    WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied. See the
#    License for the specific language governing permissions and limitations
#    under the License.

import os
import logging

log = logging.getLogger(__name__)

def terminate(exit_code=1):  # pragma: nocover
    """
    Log an Exception and terminate ACL Manager

    This MUST only be called from inside an exception handler.
    """
    log.exception("ACL Manager terminating")
    os._exit(exit_code)
